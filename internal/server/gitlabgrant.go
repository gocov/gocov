package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/forge"
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
	if s.glConnect == nil {
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
		Value:    state + "|" + ws.Prefix,
		Path:     "/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.glConnect.AuthorizeURL(state, s.redirectURI("gitlab")), http.StatusFound)
}

// gitlabConnectCallback reports whether the sign-in callback request is
// really a returning connect consent, and handles it when so. The
// connect grant shares the sign-in callback URL — GitLab enforces an
// exact redirect-URI match against the application's registered URIs —
// and the connect state cookie is what tells the two flows apart.
func (s *Server) gitlabConnectCallback(w http.ResponseWriter, r *http.Request) bool {
	if s.glConnect == nil {
		return false
	}
	c, err := r.Cookie(glConnectStateCookie)
	if err != nil {
		return false
	}
	state, prefix, _ := strings.Cut(c.Value, "|")
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
			"The consent redirect could not be validated. Start again from the workspace settings page.", "")
		return true
	}
	u := s.sessionUser(r)
	if u == nil {
		s.renderConnect(w, r, http.StatusForbidden, "Sign in first",
			"Your gocov session expired during the consent. Sign in and start the connect again "+
				"from the workspace settings page.", "")
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

	grant, err := s.glConnect.Exchange(r.Context(), code, s.redirectURI("gitlab"))
	if err != nil {
		s.log.Error("gitlab connect exchange", "workspace", ws.Prefix, "err", err)
		s.renderConnect(w, r, http.StatusBadGateway, "GitLab did not confirm the grant",
			"The authorization could not be completed with GitLab. Start again from the "+
				"workspace settings page.", "")
		return true
	}
	if err := s.store.SetWorkspaceGitLabGrant(r.Context(), ws.ID, grant.Account, grant.RefreshToken, false); err != nil {
		s.internalError(w, "storing workspace grant", err)
		return true
	}
	s.glTokens.put(ws.ID, grant.AccessToken, grant.TTL)
	s.log.Info("gitlab workspace connected", "workspace", ws.Prefix, "account", grant.Account, "user", u.DisplayName)
	http.Redirect(w, r, workspaceURL(ws.Prefix, "?connected=1"), http.StatusSeeOther)
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
	s.glTokens.drop(ws.ID)
	s.log.Info("gitlab workspace disconnected", "workspace", ws.Prefix, "user", currentUser(r).DisplayName)
	http.Redirect(w, r, workspaceURL(ws.Prefix, "?saved=1"), http.StatusSeeOther)
}

// gitlabGrantForge returns the grant-backed client when the workspace is
// connected — the GitLab half of the credential chain's top link. A
// revoked grant marks the connection broken (lazy detection) and returns
// nil, so the upload degrades exactly like missing credentials;
// transient trouble only logs and falls through the same way.
func (s *Server) gitlabGrantForge(ctx context.Context, ws *store.Workspace, forgeName string) forge.Forge {
	if s.glConnect == nil || ws == nil || forgeName != "gitlab" ||
		ws.Forge != "gitlab" || ws.GitLabGrantAccount == "" {
		return nil
	}
	token, err := s.gitlabAccessToken(ctx, ws)
	if err != nil {
		if errors.Is(err, forge.ErrCredentialsRevoked) {
			s.markGitLabGrantBroken(ctx, ws, err)
		} else {
			s.log.Warn("gitlab grant token", "workspace", ws.Prefix, "err", err)
		}
		return nil
	}
	return s.glConnect.ForgeClient(token)
}

// gitlabAccessToken returns a live access token for the workspace's
// grant, refreshing when the in-memory cache is empty or near expiry.
// Refreshes are serialized per workspace and re-read the stored refresh
// token under the lock, because every refresh rotates it: the rotated
// token is persisted (narrow UPDATE, broken flag cleared) before the
// access token is handed out.
func (s *Server) gitlabAccessToken(ctx context.Context, ws *store.Workspace) (string, error) {
	lock := s.glTokens.lock(ws.ID)
	lock.Lock()
	defer lock.Unlock()

	if token, ok := s.glTokens.get(ws.ID); ok {
		return token, nil
	}
	// The freshest stored token — a request holding the lock before us
	// may have rotated it since our caller read the workspace.
	fresh, err := s.store.WorkspaceByPrefix(ctx, ws.Prefix)
	if err != nil {
		return "", err
	}
	if fresh.GitLabRefreshToken == "" {
		// Disconnected under our feet, or the stored token could not be
		// decrypted (rotated GOCOV_SECRET_KEY) — either way a reconnect
		// is the fix.
		return "", fmt.Errorf("%w: workspace %s has no usable grant", forge.ErrCredentialsRevoked, ws.Prefix)
	}
	grant, err := s.glConnect.Refresh(ctx, fresh.GitLabRefreshToken, s.redirectURI("gitlab"))
	if err != nil {
		return "", err
	}
	newRefresh := grant.RefreshToken
	if newRefresh == "" {
		// Defensive: a non-rotating answer keeps the stored token.
		newRefresh = fresh.GitLabRefreshToken
	}
	if err := s.store.SetWorkspaceGitLabGrant(ctx, ws.ID, fresh.GitLabGrantAccount, newRefresh, false); err != nil {
		// The old token is already invalidated by the rotation; losing
		// the new one breaks the next refresh, not this upload — loud
		// log so the operator sees it before the cache runs out.
		s.log.Error("persisting rotated gitlab refresh token", "workspace", ws.Prefix, "err", err)
	}
	s.glTokens.put(ws.ID, grant.AccessToken, grant.TTL)
	return grant.AccessToken, nil
}

// markGitLabGrantBroken records the revoked grant so the settings page
// shows "reconnect". The account name is kept — it says who to replace.
func (s *Server) markGitLabGrantBroken(ctx context.Context, ws *store.Workspace, cause error) {
	s.log.Warn("gitlab grant revoked", "workspace", ws.Prefix,
		"account", ws.GitLabGrantAccount, "err", cause)
	if ws.GitLabGrantBroken {
		return
	}
	if err := s.store.SetWorkspaceGitLabGrant(ctx, ws.ID,
		ws.GitLabGrantAccount, ws.GitLabRefreshToken, true); err != nil {
		s.log.Error("marking gitlab grant broken", "workspace", ws.Prefix, "err", err)
	}
}

// addGitLabGrantData fills the GitLab connection state shared by the
// settings and setup pages. Absent when the deployment has no connect
// support or the workspace is not on GitLab.
func (s *Server) addGitLabGrantData(ws *store.Workspace, data map[string]any) {
	if s.glConnect == nil || ws.Forge != "gitlab" {
		return
	}
	data["GitLabConnect"] = true
	data["GLGrantAccount"] = ws.GitLabGrantAccount
	data["GLGrantBroken"] = ws.GitLabGrantBroken
}
