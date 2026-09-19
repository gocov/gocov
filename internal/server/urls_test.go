package server

import (
	"testing"

	"github.com/gocov/gocov/internal/store"
)

// The tenant URL builders pin the escaping split the live mux depends on:
// repo slugs ride bare as the trailing {slug...} wildcard, workspace
// prefixes are one escaped segment (a GitLab subgroup's slash becomes
// %2F), and a workspace's dashboard link escapes the whole forge/prefix
// pair into one query value. httptest preserves an unescaped slash where
// the live mux does not, so page tests alone cannot catch a builder that
// gets this wrong.
func TestTenantURLs(t *testing.T) {
	repo := &store.Repo{Forge: "gitlab", Slug: "grp/sub/proj"}
	for name, got := range map[string]string{
		"repo":            repoURL(repo),
		"repo path":       repoPath("github", "acme/widgets"),
		"badge":           badgeURL(repo),
		"workspace":       workspaceURL(&store.Workspace{Forge: "gitlab", Prefix: "grp/sub"}, "/setup"),
		"workspace plain": workspaceURL(&store.Workspace{Forge: "bitbucket", Prefix: "acme"}, ""),
		"workspace path":  workspacePath("gitlab", "grp/sub"),
		"workspace home":  workspaceHomeURL(&store.Workspace{Forge: "gitlab", Prefix: "grp/sub"}),
	} {
		want := map[string]string{
			"repo":            "/repos/gitlab/grp/sub/proj",
			"repo path":       "/repos/github/acme/widgets",
			"badge":           "/badge/gitlab/grp/sub/proj.svg",
			"workspace":       "/workspaces/gitlab/grp%2Fsub/setup",
			"workspace plain": "/workspaces/bitbucket/acme",
			"workspace path":  "/workspaces/gitlab/grp%2Fsub",
			"workspace home":  "/?ws=gitlab%2Fgrp%2Fsub",
		}[name]
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}
