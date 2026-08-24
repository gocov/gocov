package server

import (
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/store"
)

func TestMaskToken(t *testing.T) {
	// The onboarding page shows a token it must not re-reveal: only the
	// last four characters survive, and a token too short to mask that way
	// is hidden completely rather than partially.
	for _, tc := range []struct {
		tok  string
		want string
	}{
		{"", ""},
		{"abcd", ""},
		{"abcde", "bcde"},
		{"gcv_1234567890abcdef", "cdef"},
	} {
		got := maskToken(tc.tok)
		bullets := strings.Repeat("•", 24)
		if !strings.HasPrefix(got, bullets) {
			t.Errorf("maskToken(%q) = %q, want it to start with the bullet run", tc.tok, got)
		}
		if tail := strings.TrimPrefix(got, bullets); tail != tc.want {
			t.Errorf("maskToken(%q) revealed %q, want %q", tc.tok, tail, tc.want)
		}
		if tc.tok != "" && strings.Contains(got, tc.tok) {
			t.Errorf("maskToken(%q) = %q leaks the whole token", tc.tok, got)
		}
	}
}

func TestReportsPostedMsg(t *testing.T) {
	// The message names the identity a build status will appear as, and
	// says nothing at all until that identity actually exists.
	for _, tc := range []struct {
		name string
		ws   *store.Workspace
		want string
	}{
		{"github app installed", &store.Workspace{Forge: "github", GitHubInstallationID: 42}, "gocov[bot]"},
		{"github not installed", &store.Workspace{Forge: "github"}, ""},
		{"bitbucket granted", &store.Workspace{Forge: "bitbucket", BitbucketGrantAccount: "acme"}, "@acme"},
		{"bitbucket no grant", &store.Workspace{Forge: "bitbucket"}, ""},
		{"gitlab granted", &store.Workspace{Forge: "gitlab", GitLabGrantAccount: "acme"}, "@acme"},
		{"gitlab no grant", &store.Workspace{Forge: "gitlab"}, ""},
		{"unknown forge", &store.Workspace{Forge: "gitea", GitHubInstallationID: 42}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := reportsPostedMsg(tc.ws)
			if tc.want == "" {
				if got != "" {
					t.Errorf("reportsPostedMsg = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("reportsPostedMsg = %q, want it to name %q", got, tc.want)
			}
		})
	}
}
