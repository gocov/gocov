package config

// CLI is the gocov uploader's configuration. Every field is a default
// only: the matching -flag wins whenever it is given.
type CLI struct {
	Server string `env:"GOCOV_SERVER"`
	Token  string `env:"GOCOV_TOKEN"`
	Part   string `env:"GOCOV_PART"`

	// UploaderKind is set by the gocov-action so the upload page can tell
	// action uploads from bare CLI ones; read it through Kind.
	UploaderKind string `env:"GOCOV_UPLOADER_KIND"`
}

// LoadCLI reads the uploader configuration from the process environment.
func LoadCLI() (CLI, error) { return LoadCLIFrom(nil) }

// LoadCLIFrom reads the uploader configuration from an explicit
// environment map; a nil map means the process environment.
func LoadCLIFrom(environ map[string]string) (CLI, error) {
	return parse[CLI](environ)
}

// Kind is the uploader kind reported to the server: the gocov-action sets
// GOCOV_UPLOADER_KIND=action, and anything else is the bare CLI.
func (c CLI) Kind() string {
	if c.UploaderKind == "action" {
		return "action"
	}
	return "cli"
}
