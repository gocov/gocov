package config

import "time"

// Preview configures the gocov-preview dev harness. It is not part of the
// product, so these are deliberately not documented for users.
type Preview struct {
	// Auth adds fake sign-in so the login, registration and settings
	// pages are previewable.
	Auth bool `env:"GOCOV_PREVIEW_AUTH"`
	// Port is overridable so several sessions can run their own preview
	// side by side; the default matches .claude/launch.json.
	Port string `env:"PORT" default:"8099"`
	// PostHogKey renders the analytics snippet against the EU cloud, for
	// eyeballing the wiring; a bogus key exercises everything but ingest.
	PostHogKey string `env:"GOCOV_PREVIEW_POSTHOG_KEY"`
	// Now pins the clock the synthetic history is seeded against (RFC
	// 3339), so the docs screenshots come out the same on every run.
	// Unset seeds against the real clock.
	Now time.Time `env:"GOCOV_PREVIEW_NOW"`
}

// LoadPreview reads the harness configuration from the process environment.
func LoadPreview() (Preview, error) {
	return parse[Preview](nil)
}
