package server

import (
	"net/http"
	"testing"

	"github.com/gocov/gocov/internal/store"
)

// The tenant URL builders pin what the live mux depends on: repo slugs and
// workspace prefixes both ride bare as a trailing wildcard, so a GitLab
// subgroup's slash is a slash everywhere and no URL carries a %2F for a
// proxy to decode. httptest preserves an escaped slash where the live mux
// does not, so page tests alone cannot catch a builder that gets this
// wrong.
func TestTenantURLs(t *testing.T) {
	repo := &store.Repo{Forge: "gitlab", Slug: "grp/sub/proj"}
	for name, got := range map[string]string{
		"repo":               repoURL(repo),
		"repo path":          repoPath("github", "acme/widgets"),
		"badge":              badgeURL(repo),
		"workspace home":     workspaceHomeURL(&store.Workspace{Forge: "gitlab", Prefix: "grp/sub"}),
		"workspace settings": workspaceSettingsPath("gitlab", "grp/sub"),
		"workspace plain":    workspaceSettingsPath("bitbucket", "acme"),
		"workspace setup":    workspaceSetupPath("gitlab", "grp/sub"),
		"workspace connect":  workspaceConnectURL(&store.Workspace{Forge: "gitlab", Prefix: "grp/sub"}),
	} {
		want := map[string]string{
			"repo":               "/repos/gitlab/grp/sub/proj",
			"repo path":          "/repos/github/acme/widgets",
			"badge":              "/badge/gitlab/grp/sub/proj.svg",
			"workspace home":     "/w/gitlab/grp/sub",
			"workspace settings": "/workspace-settings/gitlab/grp/sub",
			"workspace plain":    "/workspace-settings/bitbucket/acme",
			"workspace setup":    "/workspace-setup/gitlab/grp/sub",
			"workspace connect":  "/workspace-connect/gitlab/grp/sub",
		}[name]
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// The addresses the workspace pages had through v0.25 still answer, with a
// permanent redirect to where the page lives now — whatever else the query
// carried (a one-shot ?error= code) comes along.
func TestLegacyWorkspaceURLsRedirect(t *testing.T) {
	f := newFixture(t, nil)
	for path, want := range map[string]string{
		"/?ws=github%2Facme":                              "/w/github/acme",
		"/?ws=gitlab%2Fgrp%2Fsub":                         "/w/gitlab/grp/sub",
		"/?ws=gitlab/grp/sub":                             "/w/gitlab/grp/sub",
		"/?ws=github%2Facme&error=connect_failed":         "/w/github/acme?error=connect_failed",
		"/workspaces/bitbucket/acme":                      "/workspace-settings/bitbucket/acme",
		"/workspaces/gitlab/grp%2Fsub":                    "/workspace-settings/gitlab/grp/sub",
		"/workspaces/gitlab/grp%2Fsub/setup":              "/workspace-setup/gitlab/grp/sub",
		"/workspaces/bitbucket/acme?error=connect_failed": "/workspace-settings/bitbucket/acme?error=connect_failed",
	} {
		rec := get(f, path)
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != want {
			t.Errorf("%s: %d -> %q, want 301 -> %q", path, rec.Code, rec.Header().Get("Location"), want)
		}
	}
	// A ?ws= that names no workspace is not a link to follow: the dashboard
	// answers as it does without one.
	for _, path := range []string{"/", "/?ws=", "/?ws=acme", "/?error=connect_denied"} {
		if rec := get(f, path); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want the dashboard shell", path, rec.Code)
		}
	}
}
