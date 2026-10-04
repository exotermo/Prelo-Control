// Package filestore keeps project files encrypted at rest (Fase PA).
//
// Format: the plaintext is cut into fixed-size chunks (1 MiB). Each chunk is sealed with
// AES-256-GCM under a per-file data key (HKDF-SHA256 of the master key with a random 32-byte salt)
// and the nonce noncePrefix(7) || counter(4, big endian) || lastFlag(1); the file id is the
// associated data. This is the STREAM construction: reordering or dropping chunks breaks the
// counter, truncating at a chunk boundary leaves a chunk that was not sealed as "last", and
// moving a blob to another file id fails authentication — all detected on read, while neither
// direction ever holds more than one chunk in memory.
package filestore

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
)

const (
	ChunkSize      = 1 << 20
	saltSize       = 32
	noncePrefixLen = 7
	tagSize        = 16
	hkdfInfo       = "prelo/project-file/v1:"
)

var (
	ErrTooLarge  = errors.New("file exceeds the size limit")
	ErrCorrupted = errors.New("stored file failed authentication")
)

// Params is what must be kept (in the database) to read a sealed file back. None of it is secret.
type Params struct {
	Salt        []byte
	NoncePrefix []byte
	ChunkSize   int
}

func deriveKey(master, salt []byte, fileID string) []byte {
	extract := hmac.New(sha256.New, salt)
	extract.Write(master)
	prk := extract.Sum(nil)
	expand := hmac.New(sha256.New, prk)
	expand.Write([]byte(hkdfInfo + fileID))
	expand.Write([]byte{0x01})
	return expand.Sum(nil)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(prefix []byte, counter uint32, last bool) []byte {
	n := make([]byte, 12)
	copy(n, prefix)
	binary.BigEndian.PutUint32(n[noncePrefixLen:], counter)
	if last {
		n[11] = 1
	}
	return n
}

// SealStream encrypts src into dst. It stops with ErrTooLarge as soon as more than maxBytes of
// plaintext have been read. It reports the plaintext size, its SHA-256 and the first bytes
// (for content-type sniffing) without ever buffering more than one chunk.
func SealStream(master []byte, fileID string, src io.Reader, dst io.Writer, maxBytes int64) (Params, int64, []byte, []byte, error) {
	params := Params{Salt: make([]byte, saltSize), NoncePrefix: make([]byte, noncePrefixLen), ChunkSize: ChunkSize}
	if _, err := rand.Read(params.Salt); err != nil {
		return Params{}, 0, nil, nil, err
	}
	if _, err := rand.Read(params.NoncePrefix); err != nil {
		return Params{}, 0, nil, nil, err
	}
	aead, err := newAEAD(deriveKey(master, params.Salt, fileID))
	if err != nil {
		return Params{}, 0, nil, nil, err
	}

	in := bufio.NewReaderSize(src, ChunkSize)
	hash := sha256.New()
	buf := make([]byte, ChunkSize)
	var head []byte
	var total int64
	for counter := uint32(0); ; counter++ {
		n, readErr := io.ReadFull(in, buf)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return Params{}, 0, nil, nil, readErr
		}
		total += int64(n)
		if total > maxBytes {
			return Params{}, 0, nil, nil, ErrTooLarge
		}
		chunk := buf[:n]
		hash.Write(chunk)
		if head == nil {
			head = append([]byte(nil), chunk[:min(n, 512)]...)
		}
		last := readErr != nil
		if !last {
			if _, peekErr := in.Peek(1); peekErr == io.EOF {
				last = true
			}
		}
		if _, err := dst.Write(aead.Seal(nil, nonce(params.NoncePrefix, counter, last), chunk, []byte(fileID))); err != nil {
			return Params{}, 0, nil, nil, err
		}
		if last {
			return params, total, hash.Sum(nil), head, nil
		}
		if counter == ^uint32(0) {
			return Params{}, 0, nil, nil, ErrTooLarge
		}
	}
}

// OpenStream returns a reader that decrypts and authenticates chunk by chunk. A tampered,
// reordered, truncated or misplaced blob surfaces as ErrCorrupted from Read.
func OpenStream(master []byte, fileID string, params Params, src io.Reader) (io.Reader, error) {
	if len(params.Salt) != saltSize || len(params.NoncePrefix) != noncePrefixLen || params.ChunkSize <= 0 {
		return nil, ErrCorrupted
	}
	aead, err := newAEAD(deriveKey(master, params.Salt, fileID))
	if err != nil {
		return nil, err
	}
	return &openReader{aead: aead, fileID: []byte(fileID), params: params,
		in: bufio.NewReaderSize(src, params.ChunkSize+tagSize), buf: make([]byte, params.ChunkSize+tagSize)}, nil
}

type openReader struct {
	aead    cipher.AEAD
	fileID  []byte
	params  Params
	in      *bufio.Reader
	buf     []byte
	plain   []byte
	counter uint32
	done    bool
	err     error
}

func (r *openReader) Read(p []byte) (int, error) {
	for len(r.plain) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		if r.done {
			return 0, io.EOF
		}
		r.next()
	}
	n := copy(p, r.plain)
	r.plain = r.plain[n:]
	return n, nil
}

func (r *openReader) next() {
	n, err := io.ReadFull(r.in, r.buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		r.err = err
		return
	}
	last := err != nil
	if !last {
		if _, peekErr := r.in.Peek(1); peekErr == io.EOF {
			last = true
		}
	}
	if n < tagSize {
		r.err = ErrCorrupted
		return
	}
	plain, openErr := r.aead.Open(nil, nonce(r.params.NoncePrefix, r.counter, last), r.buf[:n], r.fileID)
	if openErr != nil {
		r.err = ErrCorrupted
		return
	}
	r.plain = plain
	r.counter++
	r.done = last
}
