package webui

import (
	"bytes"
	"errors"
	"io/fs"
	"testing"
)

// The embedded build depends on whether web/ was built before go test,
// so the tests check that FS and Index agree rather than expecting either.

func TestFSIsRootedAtDist(t *testing.T) {
	if _, err := fs.ReadDir(FS(), "."); err != nil {
		t.Fatalf("reading FS() root: %v", err)
	}
	if _, err := fs.Stat(FS(), "dist"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("FS() has a dist/ entry, want the contents of dist/ at its root (err: %v)", err)
	}
}

func TestIndex(t *testing.T) {
	want, err := fs.ReadFile(FS(), "index.html")
	got, ok := Index()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if ok || got != nil {
			t.Errorf("Index() = %d bytes, %v without a web build, want nil, false", len(got), ok)
		}
	case err != nil:
		t.Fatalf("reading index.html: %v", err)
	default:
		if !ok || !bytes.Equal(got, want) {
			t.Errorf("Index() = %d bytes, %v, want the embedded index.html (%d bytes), true", len(got), ok, len(want))
		}
	}
}
