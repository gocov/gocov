package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gocov/gocov/internal/store"
)

// Workspace-connect grants for Bitbucket and GitLab (One-Click Connect
// P2/D6/D7). An owner clicks Connect on the settings page, consents once
// on the forge, and the workspace from then on acts through that grant:
// statuses, PR comments, reports, diff and source fetch — no manual
// credentials. The grant's refresh token lives on the workspace row,
// encrypted at rest; posts visibly carry the connecting account (D8).
//
// Both forges rotate refresh tokens on every use: each refresh
// invalidates the stored token and returns a new one. The refresh path
// (core.Forges) serializes per workspace and persists the rotated token
// before the access token is used; the handlers here only start the
// consent, take the code back, and forget the grant on disconnect.
//
// The two forges differ in nothing but the connector, the state cookie
// that binds their consent redirect, and the workspace columns the grant
// lives in — connectGrant carries those, and one set of handlers serves
// both.
type connectGrant struct {
	// forge names the forge everywhere: store value, route segment, log
	// prefix and connector lookup.
	forge string
	// cookie binds the consent redirect to the browser that started it,
	// and carries the workspace prefix being connected.
	cookie string
	// set writes the grant's columns on the workspace row.
	set func(st store.Store, ctx context.Context, workspaceID int64, account, refreshToken string, broken bool) error
}

// The connect state cookies, one per forge so an in-flight consent on
// one forge cannot be mistaken for the other's.
const (
	connectStateCookie   = "gocov_connect_state"
	glConnectStateCookie = "gocov_gl_connect_state"
)

// connectGrants lists the grant-backed forges. Whether a deployment
// actually offers a grant is the connector's presence in core.Forges.
var connectGrants = []*connectGrant{
	{forge: "bitbucket", cookie: connectStateCookie, set: store.Store.SetWorkspaceBitbucketGrant},
	{forge: "gitlab", cookie: glConnectStateCookie, set: store.Store.SetWorkspaceGitLabGrant},
}

// connectGrantFor returns the grant description for a forge, nil for a
// forge that connects some other way (or not at all).
func connectGrantFor(forgeName string) *connectGrant {
	for _, g := range connectGrants {
		if g.forge == forgeName {
			return g
		}
	}
	return nil
}

// handleConnect implements GET /workspaces/{forge}/{prefix}/connect: the
// start of the connect grant — state cookie, then the forge's consent
// page. A GitHub workspace connects by installing the App instead, so it
// has no grant to start and the page does not exist.
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	// The path's forge is the workspace's forge (that is how the row is
	// looked up), so whether a grant exists is known before any query.
	g := connectGrantFor(r.PathValue("forge"))
	if g == nil {
		http.NotFound(w, r)
		return
	}
	connector := s.forges.Connector(g.forge)
	if connector == nil {
		http.NotFound(w, r)
		return
	}
	ws := s.ownerWorkspace(w, r)
	if ws == nil {
		return
	}
	state, err := newState()
	if err != nil {
		s.internalError(w, "generating connect state", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     g.cookie,
		Value:    state + "|" + ws.Prefix,
		Path:     "/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, connector.AuthorizeURL(state, s.redirectURI(g.forge)), http.StatusFound)
}

// connectCallback reports whether the sign-in callback request is
// really a returning connect consent, and handles it when so. The
// forges require the redirect URI to match the configured callback
// exactly, so the connect grant shares the sign-in callback and the
// connect state cookie is what tells the two flows apart.
func (s *Server) connectCallback(g *connectGrant, w http.ResponseWriter, r *http.Request) bool {
	connector := s.forges.Connector(g.forge)
	if connector == nil {
		return false
	}
	c, err := r.Cookie(g.cookie)
	if err != nil {
		return false
	}
	state, prefix := splitConnectState(c.Value)
	if state == "" || r.FormValue("state") != state {
		// Not this flow's redirect (a plain sign-in, or garbage); the
		// stale cookie stays until it expires or a connect finishes.
		return false
	}
	clearCookie(w, g.cookie, s.secureCookies)
	code := r.FormValue("code")
	if r.FormValue("error") != "" || code == "" || prefix == "" {
		s.log.Warn(g.forge+" connect callback rejected", "forge_error", r.FormValue("error"))
		s.connectFailed(w, r, g.forge, prefix)
		return true
	}
	settings := workspacePath(g.forge, prefix)
	u := s.sessionUser(r)
	if u == nil {
		// The session expired during the consent; sign in and start again
		// from the settings page the Connect button sits on.
		http.Redirect(w, r, loginURL(settings), http.StatusSeeOther)
		return true
	}
	ws, err := s.store.WorkspaceByPrefix(r.Context(), g.forge, prefix)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.connectDenied(w, r, g, u)
			return true
		}
		s.internalError(w, "looking up workspace", err)
		return true
	}
	role, member, err := s.seat(r.Context(), u, ws)
	if err != nil {
		s.internalError(w, "listing memberships", err)
		return true
	}
	if !member {
		s.connectDenied(w, r, g, u)
		return true
	}
	if role != store.RoleOwner {
		// The consent was started by an owner's session; being demoted
		// (or handing the redirect to a member) in between ends it here.
		// A member may read the settings page, so that is where the app
		// says whose move connecting is.
		s.log.Warn(g.forge+" connect callback denied to member", "workspace", ws.Prefix, "user", u.DisplayName)
		http.Redirect(w, r, settings+"?error=connect_owners_only", http.StatusSeeOther)
		return true
	}

	grant, err := connector.Exchange(r.Context(), code, s.redirectURI(g.forge))
	if err != nil {
		s.log.Error(g.forge+" connect exchange", "workspace", ws.Prefix, "err", err)
		s.connectFailed(w, r, g.forge, prefix)
		return true
	}
	if err := g.set(s.store, r.Context(), ws.ID, grant.Account, grant.RefreshToken, false); err != nil {
		s.internalError(w, "storing workspace grant", err)
		return true
	}
	s.forges.CacheGrantToken(g.forge, ws.ID, grant.AccessToken, grant.TTL)
	s.log.Info(g.forge+" workspace connected", "workspace", ws.Prefix, "account", grant.Account, "user", u.DisplayName)
	// A connected workspace's home is its dashboard, where the setup
	// checklist reads the connection it just gained.
	http.Redirect(w, r, workspaceHomeURL(ws), http.StatusSeeOther)
	return true
}

// connectFailed sends a consent that could not be completed back to the
// workspace's settings page, where the Connect button is, with ?error=
// for the app to explain and offer another try. A callback that never
// named a workspace has no settings page to return to; the dashboard
// carries the same notice instead.
func (s *Server) connectFailed(w http.ResponseWriter, r *http.Request, forge, prefix string) {
	dest := "/"
	if prefix != "" {
		dest = workspacePath(forge, prefix)
	}
	http.Redirect(w, r, dest+"?error=connect_failed", http.StatusSeeOther)
}

// connectDenied ends a consent that came back for a workspace the viewer
// has no seat in — or for one that is not there, which must read the same
// (D3). Neither has a settings page to show them, so the dashboard carries
// the notice, and the redirect names no workspace.
func (s *Server) connectDenied(w http.ResponseWriter, r *http.Request, g *connectGrant, u *store.User) {
	s.log.Warn(g.forge+" connect callback denied", "user", u.DisplayName)
	http.Redirect(w, r, "/?error=connect_denied", http.StatusSeeOther)
}

// disconnectWorkspace forgets the workspace's connection — the GitHub App
// installation link, or the grant — and reports whether it could. A false
// result means the answer (a 404 for a forge with no connect mechanism,
// or an internal error) is already written; the caller then writes
// nothing of its own.
func (s *Server) disconnectWorkspace(w http.ResponseWriter, r *http.Request, ws *store.Workspace) bool {
	actor := currentUser(r).DisplayName
	if ws.Forge == "github" {
		if err := s.githubDisconnect(r.Context(), ws, actor); err != nil {
			s.internalError(w, "disconnecting workspace", err)
			return false
		}
		return true
	}
	g := connectGrantFor(ws.Forge)
	if g == nil {
		tenantNotFound(w, r)
		return false
	}
	if err := g.set(s.store, r.Context(), ws.ID, "", "", false); err != nil {
		s.internalError(w, "disconnecting "+g.forge+" grant", err)
		return false
	}
	s.forges.DropGrantToken(g.forge, ws.ID)
	s.log.Info(g.forge+" workspace disconnected", "workspace", ws.Prefix, "user", actor)
	return true
}
