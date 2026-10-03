package forge

import (
	"errors"
	"testing"
)

func TestRepoNotFound(t *testing.T) {
	err := RepoNotFound("acme/api")
	if !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("RepoNotFound does not wrap ErrRepoNotFound: %v", err)
	}
	if want := "forge: repository not found: acme/api"; err.Error() != want {
		t.Errorf("RepoNotFound = %q, want %q", err, want)
	}
}

func TestFileNotFound(t *testing.T) {
	err := FileNotFound("cmd/main.go", "abc123")
	if !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("FileNotFound does not wrap ErrRepoNotFound: %v", err)
	}
	if want := "forge: repository not found: cmd/main.go at abc123"; err.Error() != want {
		t.Errorf("FileNotFound = %q, want %q", err, want)
	}
}
