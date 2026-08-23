package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Server modes. Private keeps sign-in limited to members of tracked
// workspaces; hosted opens self-service registration to any forge account.
const (
	ModePrivate = "private"
	ModeHosted  = "hosted"
)

// Server is gocov-server's configuration.
type Server struct {
	// required,notEmpty: the two together reject both an unset variable
	// and one passed through as "", which compose files and CI runners do
	// routinely. required alone would let the empty string past.
	DatabaseURL string `env:"DATABASE_URL,required,notEmpty"`
	Addr        string `env:"GOCOV_ADDR" envDefault:":8080"`
	BaseURL     string `env:"GOCOV_BASE_URL" envDefault:"http://localhost:8080"`
	Mode        string `env:"GOCOV_MODE" envDefault:"private"`

	// SecretKey is the at-rest cipher key for stored grant refresh
	// tokens; see Validate for why its shape is not negotiable.
	SecretKey string `env:"GOCOV_SECRET_KEY"`

	// AllowedWorkspaces optionally narrows which workspace/org members
	// may sign in. Empty means "members of any tracked workspace".
	AllowedWorkspaces []string `env:"GOCOV_ALLOWED_WORKSPACES" envSeparator:","`

	Bitbucket OAuthApp `envPrefix:"GOCOV_OAUTH_BITBUCKET_"`
	GitHub    OAuthApp `envPrefix:"GOCOV_OAUTH_GITHUB_"`
	GitLab    OAuthApp `envPrefix:"GOCOV_OAUTH_GITLAB_"`

	// GitHubAppPrivateKey holds either the PEM itself or a path to a PEM
	// file; the caller reads the file when it is a path.
	GitHubAppID         string `env:"GOCOV_GITHUB_APP_ID"`
	GitHubAppPrivateKey string `env:"GOCOV_GITHUB_APP_PRIVATE_KEY"`

	GitHubWebhookSecret string `env:"GOCOV_GITHUB_WEBHOOK_SECRET"`
}

// LoadServer reads and validates the server configuration from the
// process environment.
func LoadServer() (Server, error) { return LoadServerFrom(nil) }

// LoadServerFrom reads the server configuration from an explicit
// environment map. A nil map means the process environment; a non-nil
// one is used as given, which is how the tests supply an environment.
func LoadServerFrom(environ map[string]string) (Server, error) {
	cfg, err := parse[Server](environ)
	if err != nil {
		return Server{}, err
	}
	cfg.normalize()
	if err := cfg.validate(); err != nil {
		return Server{}, err
	}
	return cfg, nil
}

// normalize trims the values where surrounding whitespace is a paste
// artefact rather than part of the setting.
func (c *Server) normalize() {
	c.SecretKey = strings.TrimSpace(c.SecretKey)
	c.Mode = strings.TrimSpace(c.Mode)
	workspaces := c.AllowedWorkspaces[:0]
	for _, ws := range c.AllowedWorkspaces {
		if ws = strings.TrimSpace(ws); ws != "" {
			workspaces = append(workspaces, ws)
		}
	}
	c.AllowedWorkspaces = workspaces
	if len(c.AllowedWorkspaces) == 0 {
		c.AllowedWorkspaces = nil
	}
}

// secretKeyPattern is the required shape of GOCOV_SECRET_KEY: exactly 64
// hex characters (256 bits), either case.
var secretKeyPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// validate reports the misconfigurations that must stop the process and
// that the struct tags cannot express: a value's shape, and rules that
// span more than one variable. Presence and emptiness are the tags' job
// (see DatabaseURL), so they are not repeated here.
func (c Server) validate() error {
	// The at-rest AES key is a plain SHA-256 of GOCOV_SECRET_KEY
	// (secretbox.New), with no salt or work factor, so the value itself
	// must carry the full 256 bits of entropy — a guessable passphrase
	// would be brute-forceable offline against a leaked database. We
	// therefore refuse anything that isn't 64 hex characters at boot
	// rather than sealing tokens under a weak key. Derivation is left
	// untouched, so an existing 64-hex key keeps producing the same
	// cipher. An unset or blank value is not an error: it just leaves
	// workspace connect disabled.
	if c.SecretKey != "" && !secretKeyPattern.MatchString(c.SecretKey) {
		return errors.New("GOCOV_SECRET_KEY must be 64 hex characters.\nGenerate one with: openssl rand -hex 32")
	}
	switch c.Mode {
	case ModePrivate:
	case ModeHosted:
		// Self-service registration is meaningless without a forge
		// identity to derive claimable workspaces from; fail fast.
		if !c.SignInEnabled() {
			return errors.New("GOCOV_MODE=hosted requires a sign-in provider (set GOCOV_OAUTH_*_KEY/SECRET)")
		}
	default:
		return fmt.Errorf("GOCOV_MODE=%q: want private or hosted", c.Mode)
	}
	return nil
}

// Warnings lists the misconfigurations that are survivable: the caller
// logs them and carries on with that feature switched off. Half a
// credential pair is the whole list — it reads as "I meant to enable
// this", so staying silent about it would hide a typo'd variable name.
func (c Server) Warnings() []string {
	var warnings []string
	for _, forge := range []struct {
		name string
		app  OAuthApp
	}{
		{"BITBUCKET", c.Bitbucket},
		{"GITHUB", c.GitHub},
		{"GITLAB", c.GitLab},
	} {
		if forge.app.Partial() {
			warnings = append(warnings, fmt.Sprintf(
				"GOCOV_OAUTH_%[1]s_KEY and GOCOV_OAUTH_%[1]s_SECRET must both be set; ignoring", forge.name))
		}
	}
	if (c.GitHubAppID == "") != (c.GitHubAppPrivateKey == "") {
		warnings = append(warnings,
			"GOCOV_GITHUB_APP_ID and GOCOV_GITHUB_APP_PRIVATE_KEY must both be set; ignoring")
	}
	return warnings
}

// Hosted reports whether the instance runs in self-service mode.
func (c Server) Hosted() bool { return c.Mode == ModeHosted }

// SignInEnabled reports whether any forge can sign users in. Configuring
// an OAuth consumer/app is the switch that turns sign-in on; without one
// the UI stays open and shows a banner saying so.
func (c Server) SignInEnabled() bool {
	return c.Bitbucket.Configured() || c.GitHub.Configured() || c.GitLab.Configured()
}

// GitHubAppEnabled reports whether the GitHub App integration is fully
// configured. A half-configured pair is a warning, not an error.
func (c Server) GitHubAppEnabled() bool {
	return c.GitHubAppID != "" && c.GitHubAppPrivateKey != ""
}

// BitbucketConnectEnabled reports whether Bitbucket workspace connect is
// on. It rides the sign-in consumer and needs the at-rest cipher for the
// refresh token it stores, so both must be configured.
func (c Server) BitbucketConnectEnabled() bool {
	return c.Bitbucket.Configured() && c.SecretKey != ""
}

// GitLabConnectEnabled reports whether GitLab workspace connect is on.
// It rides the sign-in application the same way Bitbucket's does; the
// application must additionally carry the "api" scope.
func (c Server) GitLabConnectEnabled() bool {
	return c.GitLab.Configured() && c.SecretKey != ""
}
