// The forge connections a workspace has, and keeping them alive.
//
// A repo reaches its forge through its workspace's one-click connection —
// a GitHub App installation, or a Bitbucket or GitLab grant — and never
// through credentials of its own. Resolving that client is most of what
// this file does; the rest is the upkeep it needs: refreshing a grant
// before its access token expires, persisting the rotated refresh token,
// caching the access token in memory (never in the store), and marking a
// connection broken when the forge says it is gone so the settings page
// can ask for a reconnect.

package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/forge/bitbucket"
	"github.com/gocov/gocov/internal/forge/github"
	"github.com/gocov/gocov/internal/forge/gitlab"
	"github.com/gocov/gocov/internal/store"
)

// Forges owns the deployment's forge connectors and the live state that
// goes with them. The connectors are whatever the deployment configured;
// a nil one simply means that forge offers no one-click connect here.
type Forges struct {
	Store   store.Store
	Log     *slog.Logger
	BaseURL string

	GitHubApp GitHubApp
	Bitbucket BitbucketConnect
	GitLab    GitLabConnect

	bbTokens *tokenCache
	glTokens *tokenCache
}

// NewForges wires the connectors together with the token caches they
// need. The caches live for the process: they hold access tokens only,
// so losing them costs one refresh, never a reconnect.
func NewForges(st store.Store, log *slog.Logger, baseURL string, app GitHubApp, bb BitbucketConnect, gl GitLabConnect) *Forges {
	return &Forges{
		Store: st, Log: log, BaseURL: baseURL,
		GitHubApp: app, Bitbucket: bb, GitLab: gl,
		bbTokens: newTokenCache(), glTokens: newTokenCache(),
	}
}

// CacheGrantToken remembers the access token a fresh consent just
// handed out, so the first upload after connecting does not spend a
// refresh on a token we already hold. ttl is what the forge said.
func (f *Forges) CacheGrantToken(forgeName string, workspaceID int64, token string, ttl time.Duration) {
	if c := f.cacheFor(forgeName); c != nil {
		c.put(workspaceID, token, ttl)
	}
}

// DropGrantToken forgets a workspace's cached access token — on
// disconnect, so a cached token cannot outlive the grant behind it.
func (f *Forges) DropGrantToken(forgeName string, workspaceID int64) {
	if c := f.cacheFor(forgeName); c != nil {
		c.drop(workspaceID)
	}
}

func (f *Forges) cacheFor(forgeName string) *tokenCache {
	switch forgeName {
	case "bitbucket":
		return f.bbTokens
	case "gitlab":
		return f.glTokens
	}
	return nil
}

// RedirectURI is the callback a forge must be configured with, and the
// one every OAuth call has to repeat verbatim — the forges match it
// exactly. Sign-in and workspace connect share it per forge.
func RedirectURI(baseURL, forgeName string) string {
	return strings.TrimSuffix(baseURL, "/") + "/oauth/" + forgeName + "/callback"
}

// BitbucketConnect runs the Bitbucket OAuth grants for workspace
// connect. Errors wrapping forge.ErrCredentialsRevoked mean the grant
// is gone (revoked, or the refresh token aged out unused).
type BitbucketConnect interface {
	// AuthorizeURL is the consent page for the connect grant.
	AuthorizeURL(state, redirectURI string) string
	// Exchange trades the consent code for the grant, including the
	// granting account's username.
	Exchange(ctx context.Context, code, redirectURI string) (*bitbucket.Grant, error)
	// Refresh trades a refresh token for a fresh access token and — the
	// tokens rotate — a new refresh token to persist.
	Refresh(ctx context.Context, refreshToken string) (*bitbucket.Grant, error)
	// ForgeClient returns a forge client acting through the access token.
	ForgeClient(accessToken string) forge.Forge
}

// GitLabConnect runs the GitLab OAuth grants for workspace connect —
// BitbucketConnect's twin. Errors wrapping forge.ErrCredentialsRevoked
// mean the grant is gone (revoked on the account's applications page).
type GitLabConnect interface {
	// AuthorizeURL is the consent page for the connect grant (scope api).
	AuthorizeURL(state, redirectURI string) string
	// Exchange trades the consent code for the grant, including the
	// granting account's username.
	Exchange(ctx context.Context, code, redirectURI string) (*gitlab.Grant, error)
	// Refresh trades a refresh token for a fresh access token and — the
	// tokens rotate — a new refresh token to persist. GitLab's token
	// endpoint wants the redirect URI on refreshes too.
	Refresh(ctx context.Context, refreshToken, redirectURI string) (*gitlab.Grant, error)
	// ForgeClient returns a forge client acting through the access token.
	ForgeClient(accessToken string) forge.Forge
}

// GitHubApp mints installation-scoped forge clients and answers the two
// questions the connect flow needs. Errors wrapping
// forge.ErrCredentialsRevoked mean the installation (or the app's own
// credentials) no longer exists on GitHub.
type GitHubApp interface {
	// ForgeClient returns a forge client authenticated as the given
	// installation.
	ForgeClient(ctx context.Context, installationID int64) (forge.Forge, error)
	// InstallationAccount returns the login of the org or user account
	// the installation lives on.
	InstallationAccount(ctx context.Context, installationID int64) (string, error)
	// InstallURL is the app's public install page on GitHub.
	InstallURL(ctx context.Context) (string, error)
	// VerifyRunClaim checks a tokenless upload's claim against GitHub,
	// authenticated as the installation: repo public, workflow run real
	// and in progress, PR open at the claimed head. A
	// *github.ClaimRejectedError is a definitive verdict; any other
	// error is transient.
	VerifyRunClaim(ctx context.Context, installationID int64, claim github.RunClaim) error
}

// tokenLeeway retires cached access tokens before their 2h expiry.
const tokenLeeway = 5 * time.Minute

// tokenCache holds grant access tokens in memory — never the store —
// and a per-workspace mutex that serializes refreshes.
type tokenCache struct {
	mu     sync.Mutex
	tokens map[int64]cachedToken
	locks  map[int64]*sync.Mutex
}

type cachedToken struct {
	value     string
	expiresAt time.Time
}

func newTokenCache() *tokenCache {
	return &tokenCache{tokens: map[int64]cachedToken{}, locks: map[int64]*sync.Mutex{}}
}

func (c *tokenCache) get(workspaceID int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tokens[workspaceID]
	if !ok || time.Now().After(t.expiresAt.Add(-tokenLeeway)) {
		return "", false
	}
	return t.value, true
}

func (c *tokenCache) put(workspaceID int64, token string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[workspaceID] = cachedToken{value: token, expiresAt: time.Now().Add(ttl)}
}

func (c *tokenCache) drop(workspaceID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tokens, workspaceID)
}

func (c *tokenCache) lock(workspaceID int64) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.locks[workspaceID]
	if !ok {
		l = &sync.Mutex{}
		c.locks[workspaceID] = l
	}
	return l
}

// For builds a forge client for the repo through the workspace's
// one-click connection (GitHub App installation, Bitbucket grant or
// GitLab grant). Returns (nil, nil) when the repo's workspace has no
// connection — there is no manual-credential fallback.
func (f *Forges) For(ctx context.Context, repo *store.Repo) (forge.Forge, error) {
	// The workspace is looked up lazily: only when a connection could
	// apply, so a forge that supports no one-click connect skips the
	// query entirely.
	if f.Capable(repo.Forge) {
		ws := f.WorkspaceFor(ctx, repo.Slug, repo.Forge)
		if fg := f.Connected(ctx, ws, repo.Forge); fg != nil {
			return fg, nil
		}
	}
	return nil, nil
}

// Capable reports whether a one-click connection could supply
// credentials for the forge — the gate for the extra workspace lookup.
func (f *Forges) Capable(forgeName string) bool {
	return (f.GitHubApp != nil && forgeName == "github") ||
		(f.Bitbucket != nil && forgeName == "bitbucket") ||
		(f.GitLab != nil && forgeName == "gitlab")
}

// Connected returns the workspace's one-click-connected client —
// GitHub App installation, Bitbucket grant or GitLab grant — or nil,
// the top link of the credential chain (D4/D7).
func (f *Forges) Connected(ctx context.Context, ws *store.Workspace, forgeName string) forge.Forge {
	if fg := f.installationForge(ctx, ws, forgeName); fg != nil {
		return fg
	}
	if fg := f.grantForge(ctx, ws, forgeName); fg != nil {
		return fg
	}
	return f.gitlabGrantForge(ctx, ws, forgeName)
}

// WorkspaceFor returns the workspace owning the slug's prefix, nil when
// there is none. Prefixes are tried longest first, so a repo below a
// registered GitLab subgroup resolves to that subgroup's workspace, not a
// same-named ancestor. A lookup failure only degrades down the credential
// chain — forge surfaces are best-effort everywhere else too. The forge
// must match: prefixes are globally unique, and a same-named workspace
// on another forge must not lend its secrets or its installation.
func (f *Forges) WorkspaceFor(ctx context.Context, slug, forgeName string) *store.Workspace {
	for _, prefix := range SlugPrefixes(slug) {
		ws, err := f.Store.WorkspaceByPrefix(ctx, prefix)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			f.Log.Error("workspace lookup", "repo", slug, "err", err)
			return nil
		}
		if ws.Forge != forgeName {
			return nil
		}
		return ws
	}
	return nil
}

// installationForge returns the App-backed client when the workspace is
// connected to a GitHub App installation — the top link of the credential
// chain (D4), which also makes check runs first-class (the App is never
// hit by the classic-PAT 403). A refused mint marks the connection broken
// (lazy uninstall detection, D3) and returns nil, so the upload degrades
// exactly like missing credentials: skipped, never failed, with stored
// tokens still honored further down the chain. A transient mint failure
// only logs and falls through the same way.
func (f *Forges) installationForge(ctx context.Context, ws *store.Workspace, forgeName string) forge.Forge {
	if f.GitHubApp == nil || ws == nil || forgeName != "github" ||
		ws.Forge != "github" || ws.GitHubInstallationID == 0 {
		return nil
	}
	fg, err := f.GitHubApp.ForgeClient(ctx, ws.GitHubInstallationID)
	if err != nil {
		if errors.Is(err, forge.ErrCredentialsRevoked) {
			f.markAppBroken(ctx, ws, err)
		} else {
			f.Log.Warn("github app installation token", "workspace", ws.Prefix, "err", err)
		}
		return nil
	}
	if ws.GitHubAppBroken {
		// Minting works again (a lifted suspension, a restored key) —
		// heal the flag so settings stops asking for a reconnect.
		ws.GitHubAppBroken = false
		if err := f.Store.UpdateWorkspace(ctx, ws); err != nil {
			f.Log.Error("clearing github app broken flag", "workspace", ws.Prefix, "err", err)
		}
	}
	return fg
}

// VerifyGitHubRunClaim verifies a tokenless upload's workflow-run claim
// through the workspace's installation. Connection upkeep matches
// installationForge: a refused mint marks the connection broken (lazy
// uninstall detection, D3) — the caller still sees the error, because
// unlike a forge push, tokenless authentication cannot degrade.
func (f *Forges) VerifyGitHubRunClaim(ctx context.Context, ws *store.Workspace, claim github.RunClaim) error {
	err := f.GitHubApp.VerifyRunClaim(ctx, ws.GitHubInstallationID, claim)
	if errors.Is(err, forge.ErrCredentialsRevoked) {
		f.markAppBroken(ctx, ws, err)
	}
	return err
}

// markAppBroken records that the workspace's installation stopped
// working so the settings page can show "reconnect" (D3). The id is
// kept — only flagged — since a reinstall arrives through the setup
// redirect and overwrites it anyway.
func (f *Forges) markAppBroken(ctx context.Context, ws *store.Workspace, cause error) {
	f.Log.Warn("github app installation revoked", "workspace", ws.Prefix,
		"installation", ws.GitHubInstallationID, "err", cause)
	if ws.GitHubAppBroken {
		return
	}
	ws.GitHubAppBroken = true
	if err := f.Store.UpdateWorkspace(ctx, ws); err != nil {
		f.Log.Error("marking github app broken", "workspace", ws.Prefix, "err", err)
	}
}

// InstallURL resolves the app's public install page, best effort: a
// GitHub hiccup must not take a settings page down with it. Empty string
// when unavailable; templates then render the state without a link.
func (f *Forges) InstallURL(ctx context.Context) string {
	if f.GitHubApp == nil {
		return ""
	}
	u, err := f.GitHubApp.InstallURL(ctx)
	if err != nil {
		f.Log.Warn("github app install url", "err", err)
		return ""
	}
	return u
}

// grantForge returns the grant-backed client when the workspace is
// connected — the Bitbucket half of the credential chain's top link
// (D4/D7). A revoked grant marks the connection broken (lazy detection,
// D7: the connecting member leaving revokes it) and returns nil, so the
// upload degrades exactly like missing credentials; transient trouble
// only logs and falls through the same way.
func (f *Forges) grantForge(ctx context.Context, ws *store.Workspace, forgeName string) forge.Forge {
	if f.Bitbucket == nil || ws == nil || forgeName != "bitbucket" ||
		ws.Forge != "bitbucket" || ws.BitbucketGrantAccount == "" {
		return nil
	}
	token, err := f.bitbucketAccessToken(ctx, ws)
	if err != nil {
		if errors.Is(err, forge.ErrCredentialsRevoked) {
			f.markGrantBroken(ctx, ws, err)
		} else {
			f.Log.Warn("bitbucket grant token", "workspace", ws.Prefix, "err", err)
		}
		return nil
	}
	return f.Bitbucket.ForgeClient(token)
}

// bitbucketAccessToken returns a live access token for the workspace's
// grant, refreshing when the in-memory cache is empty or near expiry.
// Refreshes are serialized per workspace and re-read the stored refresh
// token under the lock, because every refresh rotates it: the rotated
// token is persisted (narrow UPDATE, broken flag cleared) before the
// access token is handed out.
func (f *Forges) bitbucketAccessToken(ctx context.Context, ws *store.Workspace) (string, error) {
	lock := f.bbTokens.lock(ws.ID)
	lock.Lock()
	defer lock.Unlock()

	if token, ok := f.bbTokens.get(ws.ID); ok {
		return token, nil
	}
	// The freshest stored token — a request holding the lock before us
	// may have rotated it since our caller read the workspace.
	fresh, err := f.Store.WorkspaceByPrefix(ctx, ws.Prefix)
	if err != nil {
		return "", err
	}
	if fresh.BitbucketRefreshToken == "" {
		// Disconnected under our feet, or the stored token could not be
		// decrypted (rotated GOCOV_SECRET_KEY) — either way a reconnect
		// is the fix.
		return "", fmt.Errorf("%w: workspace %s has no usable grant", forge.ErrCredentialsRevoked, ws.Prefix)
	}
	grant, err := f.Bitbucket.Refresh(ctx, fresh.BitbucketRefreshToken)
	if err != nil {
		return "", err
	}
	newRefresh := grant.RefreshToken
	if newRefresh == "" {
		// Defensive: a non-rotating answer keeps the stored token.
		newRefresh = fresh.BitbucketRefreshToken
	}
	if err := f.Store.SetWorkspaceBitbucketGrant(ctx, ws.ID, fresh.BitbucketGrantAccount, newRefresh, false); err != nil {
		// The old token is already invalidated by the rotation; losing
		// the new one breaks the next refresh, not this upload — loud
		// log so the operator sees it before the 2h cache runs out.
		f.Log.Error("persisting rotated bitbucket refresh token", "workspace", ws.Prefix, "err", err)
	}
	f.bbTokens.put(ws.ID, grant.AccessToken, grant.TTL)
	return grant.AccessToken, nil
}

// markGrantBroken records the revoked grant so the settings page shows
// "reconnect" (D7). The account name is kept — it says who to replace.
func (f *Forges) markGrantBroken(ctx context.Context, ws *store.Workspace, cause error) {
	f.Log.Warn("bitbucket grant revoked", "workspace", ws.Prefix,
		"account", ws.BitbucketGrantAccount, "err", cause)
	if ws.BitbucketGrantBroken {
		return
	}
	if err := f.Store.SetWorkspaceBitbucketGrant(ctx, ws.ID,
		ws.BitbucketGrantAccount, ws.BitbucketRefreshToken, true); err != nil {
		f.Log.Error("marking bitbucket grant broken", "workspace", ws.Prefix, "err", err)
	}
}

// gitlabGrantForge returns the grant-backed client when the workspace is
// connected — the GitLab half of the credential chain's top link. A
// revoked grant marks the connection broken (lazy detection) and returns
// nil, so the upload degrades exactly like missing credentials;
// transient trouble only logs and falls through the same way.
func (f *Forges) gitlabGrantForge(ctx context.Context, ws *store.Workspace, forgeName string) forge.Forge {
	if f.GitLab == nil || ws == nil || forgeName != "gitlab" ||
		ws.Forge != "gitlab" || ws.GitLabGrantAccount == "" {
		return nil
	}
	token, err := f.gitlabAccessToken(ctx, ws)
	if err != nil {
		if errors.Is(err, forge.ErrCredentialsRevoked) {
			f.markGitLabGrantBroken(ctx, ws, err)
		} else {
			f.Log.Warn("gitlab grant token", "workspace", ws.Prefix, "err", err)
		}
		return nil
	}
	return f.GitLab.ForgeClient(token)
}

// gitlabAccessToken returns a live access token for the workspace's
// grant, refreshing when the in-memory cache is empty or near expiry.
// Refreshes are serialized per workspace and re-read the stored refresh
// token under the lock, because every refresh rotates it: the rotated
// token is persisted (narrow UPDATE, broken flag cleared) before the
// access token is handed out.
func (f *Forges) gitlabAccessToken(ctx context.Context, ws *store.Workspace) (string, error) {
	lock := f.glTokens.lock(ws.ID)
	lock.Lock()
	defer lock.Unlock()

	if token, ok := f.glTokens.get(ws.ID); ok {
		return token, nil
	}
	// The freshest stored token — a request holding the lock before us
	// may have rotated it since our caller read the workspace.
	fresh, err := f.Store.WorkspaceByPrefix(ctx, ws.Prefix)
	if err != nil {
		return "", err
	}
	if fresh.GitLabRefreshToken == "" {
		// Disconnected under our feet, or the stored token could not be
		// decrypted (rotated GOCOV_SECRET_KEY) — either way a reconnect
		// is the fix.
		return "", fmt.Errorf("%w: workspace %s has no usable grant", forge.ErrCredentialsRevoked, ws.Prefix)
	}
	grant, err := f.GitLab.Refresh(ctx, fresh.GitLabRefreshToken, RedirectURI(f.BaseURL, "gitlab"))
	if err != nil {
		return "", err
	}
	newRefresh := grant.RefreshToken
	if newRefresh == "" {
		// Defensive: a non-rotating answer keeps the stored token.
		newRefresh = fresh.GitLabRefreshToken
	}
	if err := f.Store.SetWorkspaceGitLabGrant(ctx, ws.ID, fresh.GitLabGrantAccount, newRefresh, false); err != nil {
		// The old token is already invalidated by the rotation; losing
		// the new one breaks the next refresh, not this upload — loud
		// log so the operator sees it before the cache runs out.
		f.Log.Error("persisting rotated gitlab refresh token", "workspace", ws.Prefix, "err", err)
	}
	f.glTokens.put(ws.ID, grant.AccessToken, grant.TTL)
	return grant.AccessToken, nil
}

// markGitLabGrantBroken records the revoked grant so the settings page
// shows "reconnect". The account name is kept — it says who to replace.
func (f *Forges) markGitLabGrantBroken(ctx context.Context, ws *store.Workspace, cause error) {
	f.Log.Warn("gitlab grant revoked", "workspace", ws.Prefix,
		"account", ws.GitLabGrantAccount, "err", cause)
	if ws.GitLabGrantBroken {
		return
	}
	if err := f.Store.SetWorkspaceGitLabGrant(ctx, ws.ID,
		ws.GitLabGrantAccount, ws.GitLabRefreshToken, true); err != nil {
		f.Log.Error("marking gitlab grant broken", "workspace", ws.Prefix, "err", err)
	}
}

// SetInstallationBroken marks every workspace on the given installation
// broken (or healed). No store index exists on the installation id, but
// the workspace set is small.
func (f *Forges) SetInstallationBroken(ctx context.Context, installationID int64, broken bool) {
	all, err := f.Store.ListWorkspaces(ctx)
	if err != nil {
		f.Log.Error("github webhook: listing workspaces", "err", err)
		return
	}
	for _, ws := range all {
		if ws.GitHubInstallationID != installationID || ws.GitHubAppBroken == broken {
			continue
		}
		ws.GitHubAppBroken = broken
		if err := f.Store.UpdateWorkspace(ctx, ws); err != nil {
			f.Log.Error("github webhook: updating workspace", "workspace", ws.Prefix, "err", err)
			continue
		}
		f.Log.Info("github installation flag set via webhook",
			"workspace", ws.Prefix, "installation", installationID, "broken", broken)
	}
}
