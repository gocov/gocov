package config

import (
	"testing"
	"time"
)

func TestLoadPreview(t *testing.T) {
	// Explicitly empty rather than unset: the developer's own shell may
	// well have PORT set, and empty must behave like unset anyway.
	t.Setenv("PORT", "")
	t.Setenv("GOCOV_PREVIEW_AUTH", "")
	t.Setenv("GOCOV_PREVIEW_NOW", "")
	cfg, err := LoadPreview()
	if err != nil {
		t.Fatalf("LoadPreview: %v", err)
	}
	if cfg.Port != "8099" || cfg.Auth || !cfg.Now.IsZero() {
		t.Errorf("got %+v, want port 8099, auth off and no pinned clock by default", cfg)
	}
	t.Setenv("GOCOV_PREVIEW_AUTH", "1")
	t.Setenv("PORT", "9000")
	t.Setenv("GOCOV_PREVIEW_NOW", "2026-06-01T12:00:00Z")
	cfg, err = LoadPreview()
	if err != nil {
		t.Fatalf("LoadPreview: %v", err)
	}
	if cfg.Port != "9000" || !cfg.Auth || !cfg.Now.Equal(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("got %+v, want port 9000, auth on and the clock pinned", cfg)
	}
}
