package server

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gocov/gocov/internal/store"
)

// In-site URLs for tenant surfaces. Repo slugs and workspace prefixes are
// unique per forge, not globally (github.com/acme and gitlab.com/acme are
// two tenants), so every URL that names one carries the forge first:
// /repos/{forge}/{slug}, /badge/{forge}/{slug}.svg, /w/{forge}/{prefix}.
// The forge segment is the store's forge
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

// A workspace's pages. GitLab namespace paths nest ("grp/sub"), so a prefix
// is a trailing {prefix...} wildcard like a repo slug and rides unescaped —
// never as one %2F segment, which a proxy in front of the server is free to
// decode or refuse. That is also why the page is named before the workspace
// rather than after it: a wildcard has to come last.

// workspaceHomePath is a workspace's home: the dashboard scoped to it,
// where the setup checklist and the first report appear.
func workspaceHomePath(forge, prefix string) string {
	return "/w/" + forge + "/" + prefix
}

// workspaceHomeURL is workspaceHomePath for a row. Everything that creates
// or connects a workspace — the claim, the GitHub App install, a finished
// grant — ends here.
func workspaceHomeURL(ws *store.Workspace) string {
	return workspaceHomePath(ws.Forge, ws.Prefix)
}

// workspaceSettingsPath is the workspace's settings page.
func workspaceSettingsPath(forge, prefix string) string {
	return "/workspace-settings/" + forge + "/" + prefix
}

// workspaceSetupPath is "Add a repository": the CI snippet for the workspace.
func workspaceSetupPath(forge, prefix string) string {
	return "/workspace-setup/" + forge + "/" + prefix
}

// workspaceConnectURL starts the Bitbucket/GitLab consent grant — a server
// route, since it is a navigation into the forge's consent screen.
func workspaceConnectURL(ws *store.Workspace) string {
	return "/workspace-connect/" + ws.Forge + "/" + ws.Prefix
}

// The addresses these pages had through v0.25: the dashboard picked its
// workspace with /?ws=forge/prefix, and the settings and setup pages lived
// under /workspaces/{forge}/{prefix} with the prefix as one escaped segment.
// Links to them are out there (bookmarks, chat messages, the forge's own
// redirect settings), so they answer with a permanent redirect.

// handleHome implements GET /: the dashboard of the viewer's first
// workspace — or, for a pre-path /?ws= link, a redirect to that
// workspace's own address, keeping whatever else the query carried.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	forge, prefix, ok := strings.Cut(q.Get("ws"), "/")
	if !ok || forge == "" || prefix == "" {
		s.handleAppPage(w, r)
		return
	}
	q.Del("ws")
	redirectWithQuery(w, r, workspaceHomePath(forge, prefix), q)
}

// handleLegacyWorkspacePage implements GET /workspaces/{forge}/{prefix}
// and GET /workspaces/{forge}/{prefix}/setup.
func (s *Server) handleLegacyWorkspacePage(w http.ResponseWriter, r *http.Request) {
	path := workspaceSettingsPath
	if strings.HasSuffix(r.URL.Path, "/setup") {
		path = workspaceSetupPath
	}
	redirectWithQuery(w, r, path(r.PathValue("forge"), r.PathValue("prefix")), r.URL.Query())
}

func redirectWithQuery(w http.ResponseWriter, r *http.Request, path string, q url.Values) {
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	http.Redirect(w, r, path, http.StatusMovedPermanently)
}
