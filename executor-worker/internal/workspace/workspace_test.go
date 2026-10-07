package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceStructuredOperations(t *testing.T) {
	w, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Mkdir("relatorios"); err != nil {
		t.Fatal(err)
	}
	if err := w.Mkdir("relatorios/hoje"); err != nil {
		t.Fatal(err)
	}
	data := "não para, gata não para"
	result, err := w.Create("relatorios/hoje/nota.txt", strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != int64(len(data)) || len(result.SHA256) != 64 {
		t.Fatal("bad file evidence")
	}
	got, err := w.Read("relatorios/hoje/nota.txt")
	if err != nil || string(got) != data {
		t.Fatalf("file was not readable: %v", err)
	}
	items, err := w.List("relatorios/hoje")
	if err != nil || len(items) != 1 || items[0].Name != "nota.txt" {
		t.Fatalf("bad listing: %v %v", items, err)
	}
	if _, err := w.Create("relatorios/hoje/nota.txt", strings.NewReader("overwrite")); err == nil {
		t.Fatal("overwrote a file")
	}
}

func TestWorkspaceRejectsTraversalAndSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "escape")); err != nil {
		t.Fatal(err)
	}
	w, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, name := range []string{"../other", "/tmp/file", "x/../../other", "a\\b", "a//b", "a/./b", "escape/stolen"} {
		if _, err := w.Create(name, strings.NewReader("x")); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "stolen")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("escaped workspace")
	}
}

func TestWorkspaceEnforcesSizeAndRemovesPartialFile(t *testing.T) {
	w, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Create("big", strings.NewReader(strings.Repeat("a", int(MaxFileBytes)+1))); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversize accepted: %v", err)
	}
	if _, err := w.Read("big"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial file survived")
	}
}
