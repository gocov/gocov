package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/gocov/gocov/internal/store"
)

// Bitbucket workspace-connect flow (One-Click Connect P2/D6/D7). A
// member clicks Connect on the settings page, consents once on
// Bitbucket, and the workspace from then on acts through that grant:
// statuses, PR comments, reports, diff and source fetch — no manual
// credentials. The grant's refresh token lives on the workspace row,
// encrypted at rest; posts visibly carry the connecting account (D8).
//
// Bitbucket rotates refresh tokens on every use (enforced 2026-05-04):
// each refresh invalidates the stored token and returns a new one, so
// the refresh path below serializes per workspace and persists the
// rotated token in a narrow atomic UPDATE before the access token is
// used. A second gocov instance sharing the database would still race
// its sibling's rotation — single-instance deployments only, for now.

// connectStateCookie binds the Bitbucket consent redirect to the browser
// that started it, and carries the workspace prefix being connected.
const connectStateCookie = "gocov_connect_state"

// handleBitbucketConnect implements GET /workspaces/{prefix}/bitbucket/connect:
// the start of the connect grant — state cookie, then Bitbucket's consent.
func (s *Server) handleBitbucketConnect(w http.ResponseWriter, r *http.Request) {
	if s.forges.Bitbucket == nil {
		http.NotFound(w, r)
		return
	}
	ws := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	if ws.Forge != "bitbucket" {
		http.NotFound(w, r)
		return
	}
	state, err := newState()
	if err != nil {
		s.internalError(w, "generating connect state", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     connectStateCookie,
		Value:    state + "|" + ws.Prefix + "|" + connectFrom(r),
		Path:     "/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.forges.Bitbucket.AuthorizeURL(state, s.redirectURI("bitbucket")), http.StatusFound)
}

// bitbucketConnectCallback reports whether the sign-in callback request
// is really a returning connect consent, and handles it when so. Live
// Bitbucket requires the redirect URI to match the consumer's configured
// callback exactly (the documented allow-extensions rule is gone), so
// the connect grant shares the sign-in callback and the connect state
// cookie is what tells the two flows apart.
func (s *Server) bitbucketConnectCallback(w http.ResponseWriter, r *http.Request) bool {
	if s.forges.Bitbucket == nil {
		return false
	}
	c, err := r.Cookie(connectStateCookie)
	if err != nil {
		return false
	}
	state, prefix, from := splitConnectState(c.Value)
	if state == "" || r.FormValue("state") != state {
		// Not this flow's redirect (a plain sign-in, or garbage); the
		// stale cookie stays until it expires or a connect finishes.
		return false
	}
	clearCookie(w, connectStateCookie, s.secureCookies)
	code := r.FormValue("code")
	if r.FormValue("error") != "" || code == "" || prefix == "" {
		s.log.Warn("bitbucket connect callback rejected", "bb_error", r.FormValue("error"))
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
	if ws.Forge != "bitbucket" || !member {
		http.NotFound(w, r)
		return true
	}

	grant, err := s.forges.Bitbucket.Exchange(r.Context(), code, s.redirectURI("bitbucket"))
	if err != nil {
		s.log.Error("bitbucket connect exchange", "workspace", ws.Prefix, "err", err)
		s.renderConnect(w, r, http.StatusBadGateway, "Bitbucket did not confirm the grant",
			"The authorization could not be completed with Bitbucket. Start again from the "+
				"workspace settings page.")
		return true
	}
	if err := s.store.SetWorkspaceBitbucketGrant(r.Context(), ws.ID, grant.Account, grant.RefreshToken, false); err != nil {
		s.internalError(w, "storing workspace grant", err)
		return true
	}
	s.forges.CacheGrantToken("bitbucket", ws.ID, grant.AccessToken, grant.TTL)
	s.log.Info("bitbucket workspace connected", "workspace", ws.Prefix, "account", grant.Account, "user", u.DisplayName)
	http.Redirect(w, r, connectDest(ws.Prefix, from), http.StatusSeeOther)
	return true
}

// handleBitbucketDisconnect implements POST /workspaces/{prefix}/bitbucket/disconnect:
// forget the grant. The consent itself lives on Bitbucket (the account's
// authorized-applications page); this only stops gocov using it and
// drops resolution back to the credential chain.
func (s *Server) handleBitbucketDisconnect(w http.ResponseWriter, r *http.Request) {
	ws := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	if err := s.store.SetWorkspaceBitbucketGrant(r.Context(), ws.ID, "", "", false); err != nil {
		s.internalError(w, "disconnecting bitbucket grant", err)
		return
	}
	s.forges.DropGrantToken("bitbucket", ws.ID)
	s.log.Info("bitbucket workspace disconnected", "workspace", ws.Prefix, "user", currentUser(r).DisplayName)
	http.Redirect(w, r, "/workspaces/"+ws.Prefix+"?saved=1", http.StatusSeeOther)
}

// addBitbucketGrantData fills the Bitbucket connection state shared by
// the settings and setup pages. Absent when the deployment has no
// connect support or the workspace is not on Bitbucket.
func (s *Server) addBitbucketGrantData(ws *store.Workspace, data map[string]any) {
	if s.forges.Bitbucket == nil || ws.Forge != "bitbucket" {
		return
	}
	data["BitbucketConnect"] = true
	data["BBGrantAccount"] = ws.BitbucketGrantAccount
	data["BBGrantBroken"] = ws.BitbucketGrantBroken
}
