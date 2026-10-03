package filestore

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
)

func key() []byte {
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	return k
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func seal(t *testing.T, master []byte, id string, plain []byte) (Params, []byte) {
	t.Helper()
	var out bytes.Buffer
	params, size, sum, _, err := SealStream(master, id, bytes.NewReader(plain), &out, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(plain)) {
		t.Fatalf("size %d != %d", size, len(plain))
	}
	want := sha256.Sum256(plain)
	if !bytes.Equal(sum, want[:]) {
		t.Fatal("sha256 mismatch")
	}
	return params, out.Bytes()
}

func open(master []byte, id string, params Params, sealed []byte) ([]byte, error) {
	r, err := OpenStream(master, id, params, bytes.NewReader(sealed))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func TestStream_RoundTripsAtChunkBoundaries(t *testing.T) {
	master := key()
	for _, n := range []int{0, 1, 511, ChunkSize - 1, ChunkSize, ChunkSize + 1, 3*ChunkSize + 7} {
		plain := randomBytes(n)
		params, sealed := seal(t, master, "file-1", plain)
		got, err := open(master, "file-1", params, sealed)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("size %d: round trip failed: %v", n, err)
		}
		if n > 64 && bytes.Contains(sealed, plain[:64]) {
			t.Fatalf("size %d: plaintext visible in sealed blob", n)
		}
	}
}

func TestStream_DetectsTamperingTruncationReorderAndMisplacement(t *testing.T) {
	master := key()
	plain := randomBytes(3*ChunkSize + 100)
	params, sealed := seal(t, master, "file-1", plain)
	chunk := ChunkSize + tagSize

	flipped := append([]byte(nil), sealed...)
	flipped[10] ^= 1
	truncated := sealed[:2*chunk] // drops the real last chunk at a chunk boundary
	reordered := append(append(append([]byte(nil), sealed[chunk:2*chunk]...), sealed[:chunk]...), sealed[2*chunk:]...)

	cases := map[string]func() ([]byte, error){
		"tampered":     func() ([]byte, error) { return open(master, "file-1", params, flipped) },
		"truncated":    func() ([]byte, error) { return open(master, "file-1", params, truncated) },
		"reordered":    func() ([]byte, error) { return open(master, "file-1", params, reordered) },
		"other-file":   func() ([]byte, error) { return open(master, "file-2", params, sealed) },
		"wrong-master": func() ([]byte, error) { return open(key(), "file-1", params, sealed) },
	}
	for name, run := range cases {
		if _, err := run(); !errors.Is(err, ErrCorrupted) {
			t.Errorf("%s: expected ErrCorrupted, got %v", name, err)
		}
	}
}

func TestStream_RefusesOversizedInput(t *testing.T) {
	var out bytes.Buffer
	_, _, _, _, err := SealStream(key(), "f", bytes.NewReader(randomBytes(ChunkSize+10)), &out, ChunkSize)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
}

func TestStream_SameContentSealsDifferentlyEachTime(t *testing.T) {
	master := key()
	plain := randomBytes(1000)
	_, a := seal(t, master, "file-1", plain)
	_, b := seal(t, master, "file-1", plain)
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same content must differ (fresh salt and nonce prefix)")
	}
}
