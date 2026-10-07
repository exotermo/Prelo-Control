package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"unicode"
)

const MaxFileBytes int64 = 16 << 20
const MaxReadBytes int64 = 32 << 10
const MaxEntries = 200

var ErrInvalidPath = errors.New("invalid workspace path")
var ErrLimit = errors.New("workspace limit exceeded")

type Entry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}
type Written struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// ValidatePath deliberately accepts only portable relative paths. os.Root then provides the
// actual filesystem boundary, including protection against symlinks escaping the workspace.
func ValidatePath(raw string, allowRoot bool) (string, error) {
	if allowRoot && (raw == "" || raw == ".") {
		return ".", nil
	}
	if raw == "" || len(raw) > 240 || strings.HasPrefix(raw, "/") || strings.Contains(raw, "\\") {
		return "", ErrInvalidPath
	}
	for _, ch := range raw {
		if unicode.IsControl(ch) {
			return "", ErrInvalidPath
		}
	}
	for _, part := range strings.Split(raw, "/") {
		if part == "" || part == "." || part == ".." {
			return "", ErrInvalidPath
		}
	}
	if path.Clean(raw) != raw {
		return "", ErrInvalidPath
	}
	return raw, nil
}

type Workspace struct{ root *os.Root }

func Open(root string) (*Workspace, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return &Workspace{root: r}, nil
}
func (w *Workspace) Close() error { return w.root.Close() }

func (w *Workspace) Mkdir(raw string) error {
	name, err := ValidatePath(raw, false)
	if err != nil {
		return err
	}
	return w.root.Mkdir(name, 0700)
}

func (w *Workspace) List(raw string) ([]Entry, error) {
	name, err := ValidatePath(raw, true)
	if err != nil {
		return nil, err
	}
	dir, err := w.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	items, err := dir.ReadDir(MaxEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(items) > MaxEntries {
		return nil, ErrLimit
	}
	out := make([]Entry, 0, len(items))
	for _, item := range items {
		entry := Entry{Name: item.Name(), IsDir: item.IsDir()}
		if info, err := item.Info(); err == nil {
			entry.Size = info.Size()
		} else {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func (w *Workspace) Read(raw string) ([]byte, error) {
	name, err := ValidatePath(raw, false)
	if err != nil {
		return nil, err
	}
	f, err := w.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalidPath
	}
	if info.Size() > MaxReadBytes {
		return nil, ErrLimit
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxReadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxReadBytes {
		return nil, ErrLimit
	}
	return data, nil
}

func (w *Workspace) Create(raw string, src io.Reader) (Written, error) {
	name, err := ValidatePath(raw, false)
	if err != nil {
		return Written{}, err
	}
	f, err := w.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Written{}, err
	}
	finished := false
	defer func() {
		_ = f.Close()
		if !finished {
			_ = w.root.Remove(name)
		}
	}()
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(src, MaxFileBytes+1))
	if err != nil {
		return Written{}, err
	}
	if size > MaxFileBytes {
		return Written{}, ErrLimit
	}
	if err := f.Sync(); err != nil {
		return Written{}, err
	}
	if err := f.Close(); err != nil {
		return Written{}, err
	}
	finished = true
	return Written{Size: size, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
