package config

import "testing"

// parse's one subtlety, exercised through Server because it is the
// richest caller: a nil map means "read the process environment", while
// an empty one means an empty environment. Both paths matter — the
// binaries take the first, every other test in this package the second.
func TestParseEnvironmentSource(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/from-process")
	t.Setenv("GOCOV_ADDR", ":9999")
	cfg, err := LoadServer()
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.DatabaseURL != "postgres://localhost/from-process" || cfg.Addr != ":9999" {
		t.Errorf("got %q / %q, want the process environment's values", cfg.DatabaseURL, cfg.Addr)
	}
	if _, err := LoadServerFrom(map[string]string{}); err == nil {
		t.Error("an empty environment map must not fall back to the process environment")
	}
}
