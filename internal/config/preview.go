package config

// Preview configures the gocov-preview dev harness. It is not part of the
// product, so these two are deliberately not documented for users.
type Preview struct {
	// Auth adds fake sign-in so the login, registration and settings
	// pages are previewable.
	Auth bool `env:"GOCOV_PREVIEW_AUTH"`
	// Port is overridable so several sessions can run their own preview
	// side by side; the default matches .claude/launch.json.
	Port string `env:"PORT" envDefault:"8099"`
}

// LoadPreview reads the harness configuration from the process environment.
func LoadPreview() (Preview, error) {
	return parse[Preview](nil)
}
