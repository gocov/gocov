package config

import "testing"

func TestLoadCLI(t *testing.T) {
	cfg, err := LoadCLIFrom(map[string]string{
		"GOCOV_SERVER": "https://gocov.example",
		"GOCOV_TOKEN":  "tok",
		"GOCOV_PART":   "backend",
	})
	if err != nil {
		t.Fatalf("LoadCLIFrom: %v", err)
	}
	if cfg.Server != "https://gocov.example" || cfg.Token != "tok" || cfg.Part != "backend" {
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
