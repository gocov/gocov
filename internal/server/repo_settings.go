package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gocov/gocov/internal/store"
)

// Repo settings page — the per-repository counterpart to workspace settings:
// coverage gates, base branch, the upload token and repo removal. Like every
// other tenant surface it is members-only; a non-member 404s so the repo's
// existence is never revealed. With auth off there are no members, so the
// pages do not exist.

// memberRepo resolves the {slug} path segment to a repo whose owning
// workspace the signed-in user belongs to, writing a 404 otherwise. It
// returns the repo and the matched workspace prefix (for back-links and
// crumbs). The slug rides as a single escaped segment because repo slugs
// contain slashes; the router decodes it via PathValue.
func (s *Server) memberRepo(w http.ResponseWriter, r *http.Request) (*store.Repo, string) {
	if !s.authEnabled() {
		http.NotFound(w, r)
		return nil, ""
	}
	u := currentUser(r)
	if u == nil {
		http.NotFound(w, r)
		return nil, ""
	}
	repo, err := s.store.RepoBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return nil, ""
	}
	if err != nil {
		s.internalError(w, "loading repo", err)
		return nil, ""
	}
	memberOf, err := s.store.ListWorkspacesForUser(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, "listing memberships", err)
		return nil, ""
	}
	// The most specific workspace the user belongs to that owns this repo.
	for _, prefix := range slugPrefixes(repo.Slug) { // longest first
		for _, ws := range memberOf {
			if ws.Forge == repo.Forge && ws.Prefix == prefix {
				return repo, prefix
			}
		}
	}
	http.NotFound(w, r)
	return nil, ""
}

// repoSettingsData assembles the template payload. The upload token lives on
// the repo row in the clear, so exposing it to members (Reveal) leaks nothing
// the DB does not already hold; MaskedToken is the default rendering.
func (s *Server) repoSettingsData(repo *store.Repo, wsPrefix, newToken, notice, errMsg string) map[string]any {
	baseURL := strings.TrimSuffix(s.baseURL, "/")
	return map[string]any{
		"Repo":          repo,
		"WSPrefix":      wsPrefix,
		"WSPrefixEsc":   url.PathEscape(wsPrefix),
		"Token":         repo.Token,
		"MaskedToken":   maskSecret(repo.Token),
		"NewToken":      newToken,
		"Notice":        notice,
		"Error":         errMsg,
		"GateActive":    gateActiveCount(repo.Gate),
		"BadgeMarkdown": fmt.Sprintf("![coverage](%s/badge/%s.svg)", baseURL, repo.Slug),
	}
}

// handleRepoSettings implements GET /repo-settings/{slug}.
func (s *Server) handleRepoSettings(w http.ResponseWriter, r *http.Request) {
	repo, prefix := s.memberRepo(w, r)
	if repo == nil {
		return
	}
	notice := ""
	if r.FormValue("saved") == "1" {
		notice = "Saved."
	}
	s.render(w, r, "repo-settings.html", s.repoSettingsData(repo, prefix, "", notice, ""))
}

// handleRepoSettingsSave implements POST /repo-settings/{slug}/save: the base
// branch and coverage gates for this repository.
func (s *Server) handleRepoSettingsSave(w http.ResponseWriter, r *http.Request) {
	repo, prefix := s.memberRepo(w, r)
	if repo == nil {
		return
	}
	branch := strings.TrimSpace(r.FormValue("default_branch"))
	if branch == "" {
		s.repoSettingsError(w, r, repo, prefix, "Base branch cannot be empty.")
		return
	}
	gate, errLabel := parseGateForm(r)
	if errLabel != "" {
		s.repoSettingsError(w, r, repo, prefix, errLabel)
		return
	}
	repo.DefaultBranch = branch
	repo.Gate = gate
	if err := s.store.UpdateRepo(r.Context(), repo); err != nil {
		s.internalError(w, "updating repo", err)
		return
	}
	http.Redirect(w, r, "/repo-settings/"+repo.Slug+"?saved=1", http.StatusSeeOther)
}

// handleRepoRotateToken implements POST /repo-settings/{slug}/rotate-token.
// The new token is shown once; the old one is dead by the time the page
// renders (single UPDATE, no grace period).
func (s *Server) handleRepoRotateToken(w http.ResponseWriter, r *http.Request) {
	repo, prefix := s.memberRepo(w, r)
	if repo == nil {
		return
	}
	token, err := newToken()
	if err != nil {
		s.internalError(w, "generating repo token", err)
		return
	}
	repo.Token = token
	if err := s.store.UpdateRepo(r.Context(), repo); err != nil {
		s.internalError(w, "rotating repo token", err)
		return
	}
	s.log.Info("repo token rotated", "slug", repo.Slug, "user", currentUser(r).DisplayName)
	s.render(w, r, "repo-settings.html", s.repoSettingsData(repo, prefix, token,
		"Token rotated — the previous token no longer works. Update your CI variable.", ""))
}

// handleRepoDelete implements POST /repo-settings/{slug}/delete: it removes the
// repo and cascades its uploads and reports (the store does the cascade).
// Uploads with the token start failing at once; nothing is changed on the forge.
func (s *Server) handleRepoDelete(w http.ResponseWriter, r *http.Request) {
	repo, prefix := s.memberRepo(w, r)
	if repo == nil {
		return
	}
	if err := s.store.DeleteRepo(r.Context(), repo.ID); err != nil {
		s.internalError(w, "deleting repo", err)
		return
	}
	s.log.Info("repo deleted", "slug", repo.Slug, "user", currentUser(r).DisplayName)
	http.Redirect(w, r, workspaceURL(prefix, ""), http.StatusSeeOther)
}

// repoSettingsError re-renders the settings page with a validation message.
func (s *Server) repoSettingsError(w http.ResponseWriter, r *http.Request, repo *store.Repo, prefix, msg string) {
	w.WriteHeader(http.StatusBadRequest)
	s.render(w, r, "repo-settings.html", s.repoSettingsData(repo, prefix, "", "", msg))
}
