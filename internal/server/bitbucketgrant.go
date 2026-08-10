package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gocov/gocov/internal/forge"
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

// bbTokenLeeway retires cached access tokens before their 2h expiry.
const bbTokenLeeway = 5 * time.Minute

// bbTokenCache holds grant access tokens in memory — never the store —
// and a per-workspace mutex that serializes refreshes.
type bbTokenCache struct {
	mu     sync.Mutex
	tokens map[int64]bbToken
	locks  map[int64]*sync.Mutex
}

type bbToken struct {
	value     string
	expiresAt time.Time
}

func newBBTokenCache() *bbTokenCache {
	return &bbTokenCache{tokens: map[int64]bbToken{}, locks: map[int64]*sync.Mutex{}}
}

func (c *bbTokenCache) get(workspaceID int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tokens[workspaceID]
	if !ok || time.Now().After(t.expiresAt.Add(-bbTokenLeeway)) {
		return "", false
	}
	return t.value, true
}

func (c *bbTokenCache) put(workspaceID int64, token string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[workspaceID] = bbToken{value: token, expiresAt: time.Now().Add(ttl)}
}

func (c *bbTokenCache) drop(workspaceID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tokens, workspaceID)
}

func (c *bbTokenCache) lock(workspaceID int64) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.locks[workspaceID]
	if !ok {
		l = &sync.Mutex{}
		c.locks[workspaceID] = l
	}
	return l
}

// handleBitbucketConnect implements GET /workspaces/{prefix}/bitbucket/connect:
// the start of the connect grant — state cookie, then Bitbucket's consent.
func (s *Server) handleBitbucketConnect(w http.ResponseWriter, r *http.Request) {
	if s.bbConnect == nil {
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
		Value:    state + "|" + ws.Prefix,
		Path:     "/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.bbConnect.AuthorizeURL(state, s.redirectURI("bitbucket")), http.StatusFound)
}

// bitbucketConnectCallback reports whether the sign-in callback request
// is really a returning connect consent, and handles it when so. Live
// Bitbucket requires the redirect URI to match the consumer's configured
// callback exactly (the documented allow-extensions rule is gone), so
// the connect grant shares the sign-in callback and the connect state
// cookie is what tells the two flows apart.
func (s *Server) bitbucketConnectCallback(w http.ResponseWriter, r *http.Request) bool {
	if s.bbConnect == nil {
		return false
	}
	c, err := r.Cookie(connectStateCookie)
	if err != nil {
		return false
	}
	state, prefix, _ := strings.Cut(c.Value, "|")
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
	if ws.Forge != "bitbucket" || !member {
		http.NotFound(w, r)
		return true
	}

	grant, err := s.bbConnect.Exchange(r.Context(), code, s.redirectURI("bitbucket"))
	if err != nil {
		s.log.Error("bitbucket connect exchange", "workspace", ws.Prefix, "err", err)
		s.renderConnect(w, r, http.StatusBadGateway, "Bitbucket did not confirm the grant",
			"The authorization could not be completed with Bitbucket. Start again from the "+
				"workspace settings page.", "")
		return true
	}
	if err := s.store.SetWorkspaceBitbucketGrant(r.Context(), ws.ID, grant.Account, grant.RefreshToken, false); err != nil {
		s.internalError(w, "storing workspace grant", err)
		return true
	}
	s.bbTokens.put(ws.ID, grant.AccessToken, grant.TTL)
	s.log.Info("bitbucket workspace connected", "workspace", ws.Prefix, "account", grant.Account, "user", u.DisplayName)
	http.Redirect(w, r, "/workspaces/"+ws.Prefix+"?connected=1", http.StatusSeeOther)
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
	s.bbTokens.drop(ws.ID)
	s.log.Info("bitbucket workspace disconnected", "workspace", ws.Prefix, "user", currentUser(r).DisplayName)
	http.Redirect(w, r, "/workspaces/"+ws.Prefix+"?saved=1", http.StatusSeeOther)
}

// grantForge returns the grant-backed client when the workspace is
// connected — the Bitbucket half of the credential chain's top link
// (D4/D7). A revoked grant marks the connection broken (lazy detection,
// D7: the connecting member leaving revokes it) and returns nil, so the
// upload degrades exactly like missing credentials; transient trouble
// only logs and falls through the same way.
func (s *Server) grantForge(ctx context.Context, ws *store.Workspace, forgeName string) forge.Forge {
	if s.bbConnect == nil || ws == nil || forgeName != "bitbucket" ||
		ws.Forge != "bitbucket" || ws.BitbucketGrantAccount == "" {
		return nil
	}
	token, err := s.bitbucketAccessToken(ctx, ws)
	if err != nil {
		if errors.Is(err, forge.ErrCredentialsRevoked) {
			s.markGrantBroken(ctx, ws, err)
		} else {
			s.log.Warn("bitbucket grant token", "workspace", ws.Prefix, "err", err)
		}
		return nil
	}
	return s.bbConnect.ForgeClient(token)
}

// bitbucketAccessToken returns a live access token for the workspace's
// grant, refreshing when the in-memory cache is empty or near expiry.
// Refreshes are serialized per workspace and re-read the stored refresh
// token under the lock, because every refresh rotates it: the rotated
// token is persisted (narrow UPDATE, broken flag cleared) before the
// access token is handed out.
func (s *Server) bitbucketAccessToken(ctx context.Context, ws *store.Workspace) (string, error) {
	lock := s.bbTokens.lock(ws.ID)
	lock.Lock()
	defer lock.Unlock()

	if token, ok := s.bbTokens.get(ws.ID); ok {
		return token, nil
	}
	// The freshest stored token — a request holding the lock before us
	// may have rotated it since our caller read the workspace.
	fresh, err := s.store.WorkspaceByPrefix(ctx, ws.Prefix)
	if err != nil {
		return "", err
	}
	if fresh.BitbucketRefreshToken == "" {
		// Disconnected under our feet, or the stored token could not be
		// decrypted (rotated GOCOV_SECRET_KEY) — either way a reconnect
		// is the fix.
		return "", fmt.Errorf("%w: workspace %s has no usable grant", forge.ErrCredentialsRevoked, ws.Prefix)
	}
	grant, err := s.bbConnect.Refresh(ctx, fresh.BitbucketRefreshToken)
	if err != nil {
		return "", err
	}
	newRefresh := grant.RefreshToken
	if newRefresh == "" {
		// Defensive: a non-rotating answer keeps the stored token.
		newRefresh = fresh.BitbucketRefreshToken
	}
	if err := s.store.SetWorkspaceBitbucketGrant(ctx, ws.ID, fresh.BitbucketGrantAccount, newRefresh, false); err != nil {
		// The old token is already invalidated by the rotation; losing
		// the new one breaks the next refresh, not this upload — loud
		// log so the operator sees it before the 2h cache runs out.
		s.log.Error("persisting rotated bitbucket refresh token", "workspace", ws.Prefix, "err", err)
	}
	s.bbTokens.put(ws.ID, grant.AccessToken, grant.TTL)
	return grant.AccessToken, nil
}

// markGrantBroken records the revoked grant so the settings page shows
// "reconnect" (D7). The account name is kept — it says who to replace.
func (s *Server) markGrantBroken(ctx context.Context, ws *store.Workspace, cause error) {
	s.log.Warn("bitbucket grant revoked", "workspace", ws.Prefix,
		"account", ws.BitbucketGrantAccount, "err", cause)
	if ws.BitbucketGrantBroken {
		return
	}
	if err := s.store.SetWorkspaceBitbucketGrant(ctx, ws.ID,
		ws.BitbucketGrantAccount, ws.BitbucketRefreshToken, true); err != nil {
		s.log.Error("marking bitbucket grant broken", "workspace", ws.Prefix, "err", err)
	}
}

// addBitbucketGrantData fills the Bitbucket connection state shared by
// the settings and setup pages. Absent when the deployment has no
// connect support or the workspace is not on Bitbucket.
func (s *Server) addBitbucketGrantData(ws *store.Workspace, data map[string]any) {
	if s.bbConnect == nil || ws.Forge != "bitbucket" {
		return
	}
	data["BitbucketConnect"] = true
	data["BBGrantAccount"] = ws.BitbucketGrantAccount
	data["BBGrantBroken"] = ws.BitbucketGrantBroken
}
