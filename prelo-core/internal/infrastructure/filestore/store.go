package filestore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// Store keeps sealed blobs on a local directory (a Docker volume), one file per id. Writes go to
// a temp file and are renamed into place only once fully sealed, so a failed or oversized upload
// never leaves a partial blob behind.
type Store struct {
	dir    string
	master []byte
}

func NewStore(dir string, master []byte) (*Store, error) {
	if len(master) != 32 {
		return nil, errors.New("files master key must be 32 bytes")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, master: append([]byte(nil), master...)}, nil
}

func (s *Store) path(id uuid.UUID) string { return filepath.Join(s.dir, id.String()+".sealed") }

// Saved describes a sealed upload.
type Saved struct {
	Params Params
	Size   int64
	SHA256 []byte
	Head   []byte
}

func (s *Store) Save(id uuid.UUID, src io.Reader, maxBytes int64) (Saved, error) {
	tmp, err := os.CreateTemp(s.dir, ".upload-*")
	if err != nil {
		return Saved{}, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	params, size, sum, head, err := SealStream(s.master, id.String(), src, tmp, maxBytes)
	if closeErr := tmp.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return Saved{}, err
	}
	// Link is atomic and refuses to replace an existing sealed blob. A retried worker upload
	// must never replace ciphertext while metadata still points to the previous salt/nonce.
	if err := os.Link(tmpName, s.path(id)); err != nil {
		return Saved{}, fmt.Errorf("finalize upload: %w", err)
	}
	return Saved{Params: params, Size: size, SHA256: sum, Head: head}, nil
}

// Open returns a decrypting reader; the caller must Close the returned closer.
func (s *Store) Open(id uuid.UUID, params Params) (io.Reader, io.Closer, error) {
	f, err := os.Open(s.path(id))
	if err != nil {
		return nil, nil, err
	}
	r, err := OpenStream(s.master, id.String(), params, f)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return r, f, nil
}

func (s *Store) Remove(id uuid.UUID) error {
	err := os.Remove(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
