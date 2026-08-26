package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/gocov/gocov/internal/store"
)

// GitLab workspace-connect flow — the Bitbucket grant's GitLab twin. A
// member clicks Connect on the settings page, consents once on GitLab
// (scope "api", a superset of sign-in's read-only consent), and the
// workspace from then on acts through that grant: statuses, MR notes,
// diff and source fetch — no manual tokens. The grant's refresh token
// lives on the workspace row, encrypted at rest; notes visibly carry
// the connecting account (the Bitbucket D8 caveat).
//
// GitLab rotates refresh tokens on every use: each refresh invalidates
// the stored token and returns a new one, so the refresh path below
// serializes per workspace and persists the rotated token before the
// access token is used — the same store-and-swap as Bitbucket, with the
// same single-instance caveat.

// glConnectStateCookie binds the GitLab consent redirect to the browser
// that started it, and carries the workspace prefix being connected.
const glConnectStateCookie = "gocov_gl_connect_state"

// handleGitLabConnect implements GET /workspaces/{prefix}/gitlab/connect:
// the start of the connect grant — state cookie, then GitLab's consent.
func (s *Server) handleGitLabConnect(w http.ResponseWriter, r *http.Request) {
	if s.forges.GitLab == nil {
		http.NotFound(w, r)
		return
	}
	ws := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	if ws.Forge != "gitlab" {
		http.NotFound(w, r)
		return
	}
	state, err := newState()
	if err != nil {
		s.internalError(w, "generating connect state", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     glConnectStateCookie,
		Value:    state + "|" + ws.Prefix + "|" + connectFrom(r),
		Path:     "/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.forges.GitLab.AuthorizeURL(state, s.redirectURI("gitlab")), http.StatusFound)
}

// gitlabConnectCallback reports whether the sign-in callback request is
// really a returning connect consent, and handles it when so. The
// connect grant shares the sign-in callback URL — GitLab enforces an
// exact redirect-URI match against the application's registered URIs —
// and the connect state cookie is what tells the two flows apart.
func (s *Server) gitlabConnectCallback(w http.ResponseWriter, r *http.Request) bool {
	if s.forges.GitLab == nil {
		return false
	}
	c, err := r.Cookie(glConnectStateCookie)
	if err != nil {
		return false
	}
	state, prefix, from := splitConnectState(c.Value)
	if state == "" || r.FormValue("state") != state {
		// Not this flow's redirect (a plain sign-in, or garbage); the
		// stale cookie stays until it expires or a connect finishes.
		return false
	}
	clearCookie(w, glConnectStateCookie, s.secureCookies)
	code := r.FormValue("code")
	if r.FormValue("error") != "" || code == "" || prefix == "" {
		s.log.Warn("gitlab connect callback rejected", "gl_error", r.FormValue("error"))
		s.renderConnect(w, r, http.StatusBadRequest, "Connect failed",
			"The consent redirect could not be validated. Start again from the workspace settings page.")
		return true
	}
	u := s.sessionUser(r)
	if u == nil {
		s.renderConnect(w, r, http.StatusForbidden, "Sign in first",
			"Your gocov session expired during the consent. Sign in and start the connect again "+
				"from the workspace settings page.")
		return true
	}
	ws, err := s.store.WorkspaceByPrefix(r.Context(), prefix)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return true
		}
		s.internalError(w, "looking up workspace", err)
		return true
	}
	member, err := s.isMember(r.Context(), u, ws)
	if err != nil {
		s.internalError(w, "listing memberships", err)
		return true
	}
	if ws.Forge != "gitlab" || !member {
		http.NotFound(w, r)
		return true
	}

	grant, err := s.forges.GitLab.Exchange(r.Context(), code, s.redirectURI("gitlab"))
	if err != nil {
		s.log.Error("gitlab connect exchange", "workspace", ws.Prefix, "err", err)
		s.renderConnect(w, r, http.StatusBadGateway, "GitLab did not confirm the grant",
			"The authorization could not be completed with GitLab. Start again from the "+
				"workspace settings page.")
		return true
	}
	if err := s.store.SetWorkspaceGitLabGrant(r.Context(), ws.ID, grant.Account, grant.RefreshToken, false); err != nil {
		s.internalError(w, "storing workspace grant", err)
		return true
	}
	s.forges.CacheGrantToken("gitlab", ws.ID, grant.AccessToken, grant.TTL)
	s.log.Info("gitlab workspace connected", "workspace", ws.Prefix, "account", grant.Account, "user", u.DisplayName)
	http.Redirect(w, r, connectDest(ws.Prefix, from), http.StatusSeeOther)
	return true
}

// handleGitLabDisconnect implements POST /workspaces/{prefix}/gitlab/disconnect:
// forget the grant. The consent itself lives on GitLab (the account's
// applications page); this only stops gocov using it and drops
// resolution back to the credential chain.
func (s *Server) handleGitLabDisconnect(w http.ResponseWriter, r *http.Request) {
	ws := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	if err := s.store.SetWorkspaceGitLabGrant(r.Context(), ws.ID, "", "", false); err != nil {
		s.internalError(w, "disconnecting gitlab grant", err)
		return
	}
	s.forges.DropGrantToken("gitlab", ws.ID)
	s.log.Info("gitlab workspace disconnected", "workspace", ws.Prefix, "user", currentUser(r).DisplayName)
	http.Redirect(w, r, workspaceURL(ws.Prefix, "?saved=1"), http.StatusSeeOther)
}

// addGitLabGrantData fills the GitLab connection state shared by the
// settings and setup pages. Absent when the deployment has no connect
// support or the workspace is not on GitLab.
func (s *Server) addGitLabGrantData(ws *store.Workspace, data map[string]any) {
	if s.forges.GitLab == nil || ws.Forge != "gitlab" {
		return
	}
	data["GitLabConnect"] = true
	data["GLGrantAccount"] = ws.GitLabGrantAccount
	data["GLGrantBroken"] = ws.GitLabGrantBroken
}
