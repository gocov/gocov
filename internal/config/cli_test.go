package config

import "testing"

func TestLoadCLI(t *testing.T) {
	cfg, err := LoadCLIFrom(map[string]string{
		"GOCOV_SERVER": "https://gocov.example",
		"GOCOV_TOKEN":  "tok",
		"GOCOV_PART":   "backend",
		"GOCOV_IGNORE": "cmd/preview/**,*_mock.go",
	})
	if err != nil {
		t.Fatalf("LoadCLIFrom: %v", err)
	}
	if cfg.Server != "https://gocov.example" || cfg.Token != "tok" || cfg.Part != "backend" || cfg.Ignore != "cmd/preview/**,*_mock.go" {
		t.Errorf("got %+v, want the environment's values", cfg)
	}
}

func TestCLIKind(t *testing.T) {
	for raw, want := range map[string]string{
		"":         "cli",
		"cli":      "cli",
		"action":   "action",
		"Action":   "cli", // only the action's exact marker counts
		"nonsense": "cli",
	} {
		cfg, err := LoadCLIFrom(map[string]string{"GOCOV_UPLOADER_KIND": raw})
		if err != nil {
			t.Fatalf("LoadCLIFrom: %v", err)
		}
		if got := cfg.Kind(); got != want {
			t.Errorf("GOCOV_UPLOADER_KIND=%q: Kind() = %q, want %q", raw, got, want)
		}
	}
}

// LoadCLI is what the binary actually calls, and its whole body is the
// nil that means "read the process environment". Worth pinning: passing
// an empty map here instead would compile, pass every other test, and
// silently stop the uploader from seeing $GOCOV_TOKEN — every CI job
// that supplies credentials through the environment would start failing
// with "upload token required".
func TestLoadCLIReadsProcessEnvironment(t *testing.T) {
	t.Setenv("GOCOV_SERVER", "https://gocov.example")
	t.Setenv("GOCOV_TOKEN", "tok")
	t.Setenv("GOCOV_PART", "backend")
	t.Setenv("GOCOV_UPLOADER_KIND", "action")

	cfg, err := LoadCLI()
	if err != nil {
		t.Fatalf("LoadCLI: %v", err)
	}
	if cfg.Server != "https://gocov.example" || cfg.Token != "tok" || cfg.Part != "backend" {
		t.Errorf("got %+v, want the process environment's values", cfg)
	}
	if got := cfg.Kind(); got != "action" {
		t.Errorf("Kind() = %q, want action", got)
	}
	// The same environment, read through an explicit empty map, must come
	// back empty — otherwise the assertions above would also pass for a
	// LoadCLI that ignored its argument.
	if empty, err := LoadCLIFrom(map[string]string{}); err != nil || empty != (CLI{}) {
		t.Errorf("LoadCLIFrom(empty) = %+v, %v; want a zero config and no error", empty, err)
	}
}
