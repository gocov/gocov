package config

import (
	"strings"
	"testing"
)

// validKey is a well-formed GOCOV_SECRET_KEY: 64 hex characters.
const validKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// minimal is the smallest environment that boots the server.
func minimal(extra map[string]string) map[string]string {
	environ := map[string]string{"DATABASE_URL": "postgres://localhost/gocov"}
	for k, v := range extra {
		environ[k] = v
	}
	return environ
}

func TestLoadServerDefaults(t *testing.T) {
	cfg, err := LoadServerFrom(minimal(nil))
	if err != nil {
		t.Fatalf("LoadServerFrom: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL = %q, want http://localhost:8080", cfg.BaseURL)
	}
	if cfg.Mode != ModePrivate || cfg.Hosted() {
		t.Errorf("Mode = %q, Hosted = %v; want private, false", cfg.Mode, cfg.Hosted())
	}
	if cfg.SignInEnabled() || cfg.GitHubAppEnabled() {
		t.Error("nothing configured, yet sign-in or the GitHub App reads as enabled")
	}
	if got := cfg.Warnings(); len(got) != 0 {
		t.Errorf("Warnings() = %q, want none", got)
	}
}

// A variable set to the empty string must be as good as unset, which is
// how the hand-rolled envOr helper this replaced behaved — compose files
// and CI runners routinely pass a variable through as "".
func TestLoadServerEmptyValueFallsBackToDefault(t *testing.T) {
	cfg, err := LoadServerFrom(minimal(map[string]string{"GOCOV_ADDR": "", "GOCOV_MODE": ""}))
	if err != nil {
		t.Fatalf("LoadServerFrom: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.Mode != ModePrivate {
		t.Errorf("Mode = %q, want private", cfg.Mode)
	}
}

// The required,notEmpty tag pair has to reject both spellings of "no
// database": the variable missing, and the variable passed through empty.
func TestLoadServerRequiresDatabaseURL(t *testing.T) {
	for _, tc := range []struct {
		name    string
		environ map[string]string
	}{
		{name: "unset", environ: map[string]string{}},
		{name: "empty", environ: map[string]string{"DATABASE_URL": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadServerFrom(tc.environ)
			if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
				t.Fatalf("error = %v, want one naming DATABASE_URL", err)
			}
		})
	}
}

func TestSecretKey(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "unset", raw: "", want: ""},
		{name: "blank is treated as unset", raw: "   \n\t ", want: ""},
		{name: "valid lowercase", raw: validKey, want: validKey},
		{name: "valid uppercase", raw: strings.ToUpper(validKey), want: strings.ToUpper(validKey)},
		{name: "trims surrounding whitespace", raw: "  " + validKey + "\n", want: validKey},
		{name: "passphrase rejected", raw: "hunter2", wantErr: true},
		{name: "too short", raw: validKey[:63], wantErr: true},
		{name: "too long", raw: validKey + "0", wantErr: true},
		{name: "non-hex char", raw: validKey[:63] + "g", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadServerFrom(minimal(map[string]string{"GOCOV_SECRET_KEY": tc.raw}))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("GOCOV_SECRET_KEY=%q: got %q, nil; want error", tc.raw, cfg.SecretKey)
				}
				// The message must name the variable and how to generate one.
				if !strings.Contains(err.Error(), "64 hex characters") ||
					!strings.Contains(err.Error(), "openssl rand -hex 32") {
					t.Errorf("error %q missing guidance", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("GOCOV_SECRET_KEY=%q: unexpected error %v", tc.raw, err)
			}
			if cfg.SecretKey != tc.want {
				t.Errorf("SecretKey = %q, want %q", cfg.SecretKey, tc.want)
			}
		})
	}
}

func TestLoadServerMode(t *testing.T) {
	oauth := map[string]string{
		"GOCOV_OAUTH_GITHUB_KEY":    "k",
		"GOCOV_OAUTH_GITHUB_SECRET": "s",
	}
	t.Run("hosted with a provider", func(t *testing.T) {
		cfg, err := LoadServerFrom(minimal(map[string]string{
			"GOCOV_MODE":                "hosted",
			"GOCOV_OAUTH_GITHUB_KEY":    oauth["GOCOV_OAUTH_GITHUB_KEY"],
			"GOCOV_OAUTH_GITHUB_SECRET": oauth["GOCOV_OAUTH_GITHUB_SECRET"],
		}))
		if err != nil {
			t.Fatalf("LoadServerFrom: %v", err)
		}
		if !cfg.Hosted() {
			t.Error("Hosted() = false, want true")
		}
	})
	t.Run("hosted without a provider", func(t *testing.T) {
		_, err := LoadServerFrom(minimal(map[string]string{"GOCOV_MODE": "hosted"}))
		if err == nil || !strings.Contains(err.Error(), "sign-in provider") {
			t.Fatalf("error = %v, want one about the missing sign-in provider", err)
		}
	})
	t.Run("unknown mode", func(t *testing.T) {
		_, err := LoadServerFrom(minimal(map[string]string{"GOCOV_MODE": "public"}))
		if err == nil || !strings.Contains(err.Error(), "private or hosted") {
			t.Fatalf("error = %v, want one listing the valid modes", err)
		}
	})
}

func TestLoadServerAllowedWorkspaces(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{raw: "", want: nil},
		{raw: "acme", want: []string{"acme"}},
		{raw: "acme,other", want: []string{"acme", "other"}},
		{raw: " acme , gl-group/platform ", want: []string{"acme", "gl-group/platform"}},
		{raw: "acme,,", want: []string{"acme"}},
		{raw: " , ", want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			cfg, err := LoadServerFrom(minimal(map[string]string{"GOCOV_ALLOWED_WORKSPACES": tc.raw}))
			if err != nil {
				t.Fatalf("LoadServerFrom: %v", err)
			}
			if len(cfg.AllowedWorkspaces) != len(tc.want) {
				t.Fatalf("AllowedWorkspaces = %q, want %q", cfg.AllowedWorkspaces, tc.want)
			}
			for i, ws := range tc.want {
				if cfg.AllowedWorkspaces[i] != ws {
					t.Fatalf("AllowedWorkspaces = %q, want %q", cfg.AllowedWorkspaces, tc.want)
				}
			}
		})
	}
}

func TestLoadServerOAuthPrefixes(t *testing.T) {
	cfg, err := LoadServerFrom(minimal(map[string]string{
		"GOCOV_OAUTH_BITBUCKET_KEY":    "bb-key",
		"GOCOV_OAUTH_BITBUCKET_SECRET": "bb-secret",
		"GOCOV_OAUTH_GITHUB_KEY":       "gh-key",
		"GOCOV_OAUTH_GITHUB_SECRET":    "gh-secret",
		"GOCOV_OAUTH_GITLAB_KEY":       "gl-key",
		"GOCOV_OAUTH_GITLAB_SECRET":    "gl-secret",
	}))
	if err != nil {
		t.Fatalf("LoadServerFrom: %v", err)
	}
	for _, forge := range []struct {
		name string
		app  OAuthApp
		key  string
	}{
		{"bitbucket", cfg.Bitbucket, "bb-key"},
		{"github", cfg.GitHub, "gh-key"},
		{"gitlab", cfg.GitLab, "gl-key"},
	} {
		if forge.app.Key != forge.key || !forge.app.Configured() {
			t.Errorf("%s = %+v, want key %q and configured", forge.name, forge.app, forge.key)
		}
	}
	if !cfg.SignInEnabled() {
		t.Error("SignInEnabled() = false, want true")
	}
	// No secret key, so neither workspace connect may turn itself on.
	if cfg.BitbucketConnectEnabled() || cfg.GitLabConnectEnabled() {
		t.Error("workspace connect enabled without GOCOV_SECRET_KEY")
	}
}

func TestLoadServerConnectNeedsSecretKey(t *testing.T) {
	cfg, err := LoadServerFrom(minimal(map[string]string{
		"GOCOV_OAUTH_BITBUCKET_KEY":    "bb-key",
		"GOCOV_OAUTH_BITBUCKET_SECRET": "bb-secret",
		"GOCOV_SECRET_KEY":             validKey,
	}))
	if err != nil {
		t.Fatalf("LoadServerFrom: %v", err)
	}
	if !cfg.BitbucketConnectEnabled() {
		t.Error("BitbucketConnectEnabled() = false, want true")
	}
	// GitLab has no OAuth application here, so its connect stays off even
	// though the shared secret key is present.
	if cfg.GitLabConnectEnabled() {
		t.Error("GitLabConnectEnabled() = true without an OAuth application")
	}
}

// Half a credential pair is survivable but must be reported: it reads as
// "I meant to enable this", so a typo'd variable name has to be visible.
func TestServerWarnings(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		wants []string
	}{
		{
			name:  "half an oauth app",
			env:   map[string]string{"GOCOV_OAUTH_GITHUB_KEY": "gh-key"},
			wants: []string{"GOCOV_OAUTH_GITHUB_KEY and GOCOV_OAUTH_GITHUB_SECRET"},
		},
		{
			name:  "secret without key",
			env:   map[string]string{"GOCOV_OAUTH_GITLAB_SECRET": "gl-secret"},
			wants: []string{"GOCOV_OAUTH_GITLAB_KEY and GOCOV_OAUTH_GITLAB_SECRET"},
		},
		{
			name:  "half a github app",
			env:   map[string]string{"GOCOV_GITHUB_APP_ID": "123"},
			wants: []string{"GOCOV_GITHUB_APP_ID and GOCOV_GITHUB_APP_PRIVATE_KEY"},
		},
		{
			name: "several at once",
			env: map[string]string{
				"GOCOV_OAUTH_BITBUCKET_KEY":    "bb-key",
				"GOCOV_GITHUB_APP_PRIVATE_KEY": "pem",
			},
			wants: []string{
				"GOCOV_OAUTH_BITBUCKET_KEY and GOCOV_OAUTH_BITBUCKET_SECRET",
				"GOCOV_GITHUB_APP_ID and GOCOV_GITHUB_APP_PRIVATE_KEY",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadServerFrom(minimal(tc.env))
			if err != nil {
				t.Fatalf("LoadServerFrom: %v", err)
			}
			got := cfg.Warnings()
			if len(got) != len(tc.wants) {
				t.Fatalf("Warnings() = %q, want %d matching %q", got, len(tc.wants), tc.wants)
			}
			for i, want := range tc.wants {
				if !strings.Contains(got[i], want) {
					t.Errorf("Warnings()[%d] = %q, want it to mention %q", i, got[i], want)
				}
			}
		})
	}
}
