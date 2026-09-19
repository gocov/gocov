package server

import (
	"net/url"

	"github.com/gocov/gocov/internal/store"
)

// In-site URLs for tenant surfaces. Repo slugs and workspace prefixes are
// unique per forge, not globally (github.com/acme and gitlab.com/acme are
// two tenants), so every URL that names one carries the forge first:
// /repos/{forge}/{slug}, /badge/{forge}/{slug}.svg,
// /workspaces/{forge}/{prefix}. The forge segment is the store's forge
// name — github, gitlab, bitbucket — not an abbreviation, so the URL, the
// row and the logs all say the same word. Handlers feed the segment
// straight into the store: a name no row carries is a plain not-found.

// repoURL is the repo's coverage page. Slugs contain slashes by
// construction ("workspace/repo"), so they ride unescaped as the trailing
// {slug...} wildcard.
func repoURL(repo *store.Repo) string {
	return repoPath(repo.Forge, repo.Slug)
}

// repoPath is repoURL for callers holding the forge and slug but no row
// (the sitemap walks store.RepoRefs).
func repoPath(forge, slug string) string {
	return "/repos/" + forge + "/" + slug
}

// badgeURL is the repo's SVG badge.
func badgeURL(repo *store.Repo) string {
	return "/badge/" + repo.Forge + "/" + repo.Slug + ".svg"
}

// workspaceURL builds an in-site link to a workspace page.
func workspaceURL(ws *store.Workspace, suffix string) string {
	return workspacePath(ws.Forge, ws.Prefix) + suffix
}

// workspacePath is workspaceURL for callers holding the forge and prefix
// but no row. The prefix is escaped into a single path segment because
// GitLab namespace paths nest ("grp/sub" → "grp%2Fsub"); the router
// decodes it back via PathValue.
func workspacePath(forge, prefix string) string {
	return "/workspaces/" + forge + "/" + url.PathEscape(prefix)
}
