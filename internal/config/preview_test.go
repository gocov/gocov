package config

import "testing"

func TestLoadPreview(t *testing.T) {
	// Explicitly empty rather than unset: the developer's own shell may
	// well have PORT set, and empty must behave like unset anyway.
	t.Setenv("PORT", "")
	t.Setenv("GOCOV_PREVIEW_AUTH", "")
	cfg, err := LoadPreview()
	if err != nil {
		t.Fatalf("LoadPreview: %v", err)
	}
	if cfg.Port != "8099" || cfg.Auth {
		t.Errorf("got %+v, want port 8099 and auth off by default", cfg)
	}
	t.Setenv("GOCOV_PREVIEW_AUTH", "1")
	t.Setenv("PORT", "9000")
	cfg, err = LoadPreview()
	if err != nil {
		t.Fatalf("LoadPreview: %v", err)
	}
	if cfg.Port != "9000" || !cfg.Auth {
		t.Errorf("got %+v, want port 9000 and auth on", cfg)
	}
}
