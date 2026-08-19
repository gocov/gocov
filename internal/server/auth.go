package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/auth"
	"github.com/gocov/gocov/internal/store"
)

// Session lifetime is fixed (no sliding renewal in M1); logout and
// `gocov-server user remove` revoke server-side immediately.
const sessionTTL = 30 * 24 * time.Hour

const (
	sessionCookie = "gocov_session"
	// stateCookie binds the OAuth callback to the browser that started the
	// flow (CSRF/replay protection). It also carries the in-site path to
	// return to after login.
	stateCookie = "gocov_oauth_state"
)

type ctxKey int

const userKey ctxKey = 0

// currentUser returns the signed-in user, or nil on public pages and when
// auth is not configured.
func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

func withUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// publicPath reports whether a path must work without a session: the CI
// surface (upload API, badges, health), embedded assets and the login
// flow itself. Everything else is a protected page.
func publicPath(p string) bool {
	if p == "/api/v1/upload" || p == "/healthz" || p == "/login" ||
		p == "/github/webhook" {
		return true
	}
	return strings.HasPrefix(p, "/badge/") ||
		strings.HasPrefix(p, "/static/") ||
		strings.HasPrefix(p, "/oauth/")
}

// authEnabled reports whether at least one sign-in provider is
// configured — the switch between the open UI and enforced sign-in.
func (s *Server) authEnabled() bool { return len(s.auths) > 0 }

// requireAuth is the enforcement middleware. With no provider configured
// the UI stays open exactly as before (the layout shows a banner instead);
// with one configured, every non-public path needs a valid session.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authEnabled() || publicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		u := s.sessionUser(r)
		if u == nil {
			redirectToLogin(w, r)
			return
		}
		// Protected pages must not be served from cache after logout.
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

// sessionUser resolves the session cookie to a user, or nil.
func (s *Server) sessionUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := s.store.UserBySession(r.Context(), hashToken(c.Value))
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Error("session lookup", "err", err)
		}
		return nil
	}
	return u
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Path
	if r.URL.RawQuery != "" {
		next += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusFound)
}

// loginProvider is one sign-in button on the login page.
type loginProvider struct {
	Name   string // forge name, the URL segment of the login routes
	Label  string // human-readable button label
	Abbrev string // two-letter mark shown on the sign-in button
}

// providerLabels maps forge names to their proper spelling; unknown
// forges fall back to their capitalized name.
var providerLabels = map[string]string{
	"bitbucket": "Bitbucket",
	"github":    "GitHub",
	"gitlab":    "GitLab",
}

// providerAbbrevs maps forge names to the two-letter mark on their button;
// unknown forges fall back to the first two letters of the name. The mark's
// colour is a per-forge CSS class (.pmark-<name>) in style.css.
var providerAbbrevs = map[string]string{
	"bitbucket": "BB",
	"github":    "GH",
	"gitlab":    "GL",
}

func providerLabel(name string) string {
	if l, ok := providerLabels[name]; ok {
		return l
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func providerAbbrev(name string) string {
	if a, ok := providerAbbrevs[name]; ok {
		return a
	}
	if len(name) >= 2 {
		return strings.ToUpper(name[:2])
	}
	return strings.ToUpper(name)
}

// providerIcons holds the inner SVG (a single 24×24 brand path, filled with
// currentColor) for each known forge's sign-in button mark. Unknown forges
// have no icon and fall back to their two-letter abbreviation in the template.
var providerIcons = map[string]string{
	"github":    `<path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23A11.509 11.509 0 0 1 12 5.803c1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222 0 1.606-.014 2.898-.014 3.293 0 .322.216.694.825.576C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"/>`,
	"bitbucket": `<path d="M.778 1.213a.768.768 0 0 0-.768.892l3.263 19.81c.084.5.515.868 1.022.873H19.95a.772.772 0 0 0 .77-.646l3.27-20.03a.768.768 0 0 0-.768-.891zM14.52 15.53H9.522L8.17 8.466h7.561z"/>`,
	"gitlab":    `<path d="M23.955 13.587l-1.342-4.135-2.664-8.189a.455.455 0 0 0-.867 0L16.418 9.45H7.582L4.919 1.263a.455.455 0 0 0-.867 0L1.388 9.452.045 13.587a.924.924 0 0 0 .331 1.023L12 23.054l11.624-8.443a.92.92 0 0 0 .331-1.024"/>`,
}

// providerIcon returns the inline SVG mark for a forge, or empty template.HTML
// when the forge is unknown. The strings are compile-time constants, never
// user input, so emitting them as trusted HTML is safe.
func providerIcon(name string) template.HTML {
	inner, ok := providerIcons[name]
	if !ok {
		return ""
	}
	return template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true">` + inner + `</svg>`)
}

// handleLogin implements GET /login — the sign-in page with one button
// per provider, doubling as the generic-failure and access-denied page.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if u := s.sessionUser(r); u != nil {
		http.Redirect(w, r, sanitizeNext(r.FormValue("next")), http.StatusFound)
		return
	}
	// The denial page is private-mode only (M3/D1): a hosted instance
	// never denies a sign-in, so the flag — and the tracked-workspace
	// disclosure below — must not be reachable by URL either.
	denied := r.FormValue("denied") == "1" && !s.hosted
	if denied {
		w.WriteHeader(http.StatusForbidden)
	}
	// The tracked-workspace slugs tell a denied member which workspace
	// to ask about, but to anyone else they disclose who uses this
	// instance — so they render only after a real Bitbucket identity
	// was rejected, never on the plain sign-in page.
	var workspaces []string
	if denied {
		workspaces = s.trackedWorkspaces(r)
	}
	providers := make([]loginProvider, 0, len(s.authOrder))
	for _, p := range s.authOrder {
		providers = append(providers, loginProvider{
			Name:   p.Name(),
			Label:  providerLabel(p.Name()),
			Abbrev: providerAbbrev(p.Name()),
		})
	}
	s.render(w, r, "login.html", map[string]any{
		"Failed":     r.FormValue("error") == "1",
		"Denied":     denied,
		"Next":       sanitizeNext(r.FormValue("next")),
		"Workspaces": workspaces,
		"Providers":  providers,
		"Hosted":     s.hosted,
	})
}

// oauthProvider resolves the {forge} path segment of the login routes,
// writing the error response itself when there is no such provider.
func (s *Server) oauthProvider(w http.ResponseWriter, r *http.Request) auth.Provider {
	if !s.authEnabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return nil
	}
	p, ok := s.auths[r.PathValue("forge")]
	if !ok {
		http.NotFound(w, r)
		return nil
	}
	return p
}

// handleOAuthStart implements GET /oauth/{forge}/start: it binds a fresh
// state to a short-lived cookie and forwards to the consent screen.
func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	provider := s.oauthProvider(w, r)
	if provider == nil {
		return
	}
	state, err := newState()
	if err != nil {
		s.internalError(w, "generating oauth state", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state + "|" + sanitizeNext(r.FormValue("next")),
		Path:     "/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, provider.AuthorizeURL(state, s.redirectURI(provider.Name())), http.StatusFound)
}

// handleOAuthCallback implements GET /oauth/{forge}/callback per D4: it
// verifies the state, exchanges the code for an identity, applies the
// workspace-membership rule and only then provisions the user and session.
// The provider discards the forge tokens; nothing forge-side is stored.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := s.oauthProvider(w, r)
	if provider == nil {
		return
	}
	// A returning workspace-connect consent shares this callback URL —
	// the forges enforce an exact redirect-URI match on the configured
	// callback — and is told apart by its own state cookie.
	if provider.Name() == "bitbucket" && s.bitbucketConnectCallback(w, r) {
		return
	}
	if provider.Name() == "gitlab" && s.gitlabConnectCallback(w, r) {
		return
	}
	failed := func() { http.Redirect(w, r, "/login?error=1", http.StatusFound) }

	state, next := readStateCookie(r)
	clearCookie(w, stateCookie, s.secureCookies)
	code := r.FormValue("code")
	if r.FormValue("error") != "" || code == "" || state == "" || r.FormValue("state") != state {
		s.log.Warn("oauth callback rejected", "forge", provider.Name(),
			"forge_error", r.FormValue("error"), "state_ok", r.FormValue("state") == state && state != "")
		failed()
		return
	}

	id, err := provider.Identity(r.Context(), code, s.redirectURI(provider.Name()))
	if err != nil {
		s.log.Error("oauth identity", "forge", provider.Name(), "err", err)
		failed()
		return
	}

	// The membership gate is private-mode only (M3/D1); a hosted instance
	// admits any forge account and routes the workspace question to the
	// registration page instead.
	if !s.hosted {
		allowed, err := s.allowedWorkspaceSet(r)
		if err != nil {
			s.internalError(w, "deriving allowed workspaces", err)
			return
		}
		member := false
		for _, ws := range id.Workspaces {
			if allowed[ws] {
				member = true
				break
			}
		}
		if !member {
			// No user row, no session (R3): denial must leave nothing behind.
			// Both sides of the failed intersection are logged so an operator
			// can see at a glance whether the fix is a missing registration,
			// a stale GOCOV_ALLOWED_WORKSPACES or a slug mismatch.
			s.log.Warn("sign-in denied", "forge", provider.Name(), "account", id.DisplayName, "email", id.Email,
				"member_of", id.Workspaces, "allowed", sortedKeys(allowed))
			http.Redirect(w, r, "/login?denied=1", http.StatusFound)
			return
		}
	}

	u := &store.User{
		Forge:       provider.Name(),
		ForgeUUID:   id.ForgeUUID,
		Email:       id.Email,
		DisplayName: id.DisplayName,
		// The workspace snapshot the registration page renders from
		// (M3/D3); refreshed on every login, stale in between.
		ForgeWorkspaces: id.Workspaces,
	}
	if err := s.store.UpsertUser(r.Context(), u); err != nil {
		s.internalError(w, "provisioning user", err)
		return
	}
	memberships, err := s.syncMemberships(r.Context(), u, id.Workspaces)
	if err != nil {
		s.internalError(w, "syncing workspace memberships", err)
		return
	}
	// A hosted user who belongs to no tracked workspace has nothing to see
	// yet — land them in the onboarding wizard rather than an empty dashboard.
	// Only when no explicit destination was requested: a re-auth aimed at a
	// pending GitHub install (next=/github/setup?...) must be honoured so it
	// can connect after the org snapshot refreshes.
	if s.hosted && memberships == 0 && (next == "" || next == "/") {
		next = "/onboarding"
	}
	token, err := newState() // same entropy requirement: 256 random bits
	if err != nil {
		s.internalError(w, "generating session token", err)
		return
	}
	sess := &store.Session{
		TokenHash: hashToken(token),
		UserID:    u.ID,
		ExpiresAt: time.Now().Add(sessionTTL),
	}
	if err := s.store.CreateSession(r.Context(), sess); err != nil {
		s.internalError(w, "creating session", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	s.log.Info("sign-in", "forge", provider.Name(), "user", u.DisplayName, "email", u.Email)
	http.Redirect(w, r, next, http.StatusFound)
}

// handleLogout implements POST /logout: the session dies server-side, so a
// saved cookie or the back button cannot restore access.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := s.store.DeleteSession(r.Context(), hashToken(c.Value)); err != nil && !errors.Is(err, store.ErrNotFound) {
			s.internalError(w, "deleting session", err)
			return
		}
	}
	clearCookie(w, sessionCookie, s.secureCookies)
	if !s.authEnabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

// allowedWorkspaceSet is the D3 authorization rule: the operator's explicit
// GOCOV_ALLOWED_WORKSPACES list when set, otherwise the workspaces this
// instance tracks (registered workspace prefixes plus the workspace part
// of every registered repo slug).
func (s *Server) allowedWorkspaceSet(r *http.Request) (map[string]bool, error) {
	set := map[string]bool{}
	if len(s.allowedWorkspaces) > 0 {
		for _, ws := range s.allowedWorkspaces {
			set[ws] = true
		}
		return set, nil
	}
	workspaces, err := s.store.ListWorkspaces(r.Context())
	if err != nil {
		return nil, err
	}
	for _, ws := range workspaces {
		set[ws.Prefix] = true
	}
	repos, err := s.store.ListRepos(r.Context())
	if err != nil {
		return nil, err
	}
	for _, repo := range repos {
		for _, prefix := range slugPrefixes(repo.Slug) {
			set[prefix] = true
		}
	}
	return set, nil
}

// slugPrefixes returns every slash-boundary prefix of a repo slug,
// longest first: "a/b/c" → ["a/b", "a"]. GitLab namespaces nest, so a
// repo's workspace can sit at any depth (a registered subgroup path is a
// workspace of its own); Bitbucket and GitHub slugs only ever have the
// single-segment prefix.
func slugPrefixes(slug string) []string {
	var out []string
	for i := len(slug); ; {
		j := strings.LastIndex(slug[:i], "/")
		if j < 0 {
			return out
		}
		out = append(out, slug[:j])
		i = j
	}
}

// syncMemberships persists the user's workspace memberships (M2/D2): the
// tracked workspaces on this forge whose prefix the forge reports the user
// belongs to. It runs on every sign-in with full-sync semantics, so a
// membership the forge no longer reports is dropped at the next login.
// Matching is per provider (forge + prefix), so two forges sharing a prefix
// stay distinct tenants. Returns how many memberships the user ends up
// with, which decides a hosted user's post-login landing page.
func (s *Server) syncMemberships(ctx context.Context, u *store.User, forgeWorkspaces []string) (int, error) {
	forgeSet := make(map[string]bool, len(forgeWorkspaces))
	for _, ws := range forgeWorkspaces {
		forgeSet[ws] = true
	}
	tracked, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for _, ws := range tracked {
		if ws.Forge == u.Forge && forgeSet[ws.Prefix] {
			ids = append(ids, ws.ID)
		}
	}
	return len(ids), s.store.SetUserWorkspaces(ctx, u.ID, ids)
}

// repoScope captures which repos a request may see (M2/R3). When scoped is
// false the instance runs in open mode (D5) and every repo is visible;
// otherwise a repo is visible only when its workspace prefix is a member
// prefix.
type repoScope struct {
	scoped   bool
	prefixes map[string]bool
}

// allows reports whether a namespaced repo slug falls within the scope.
// Any slash-boundary prefix may carry the membership — a GitLab workspace
// registered at subgroup depth covers the projects below it.
func (rs repoScope) allows(slug string) bool {
	if !rs.scoped {
		return true
	}
	for _, prefix := range slugPrefixes(slug) {
		if rs.prefixes[prefix] {
			return true
		}
	}
	return false
}

// userScope resolves the request user's workspace membership into a scope.
// Auth off → unscoped, so an open-mode instance behaves exactly as before
// M2. Auth on → the user's workspace prefixes; a missing user (which should
// not occur behind requireAuth) yields a deny-all scope rather than an open
// one.
func (s *Server) userScope(r *http.Request) (repoScope, error) {
	if !s.authEnabled() {
		return repoScope{scoped: false}, nil
	}
	prefixes := map[string]bool{}
	if u := currentUser(r); u != nil {
		wss, err := s.store.ListWorkspacesForUser(r.Context(), u.ID)
		if err != nil {
			return repoScope{}, err
		}
		for _, ws := range wss {
			prefixes[ws.Prefix] = true
		}
	}
	return repoScope{scoped: true, prefixes: prefixes}, nil
}

// canView reports whether the request may see the given repo slug. Callers
// that fail the check 404 (D3: a non-member must not learn a repo exists).
func (s *Server) canView(r *http.Request, slug string) (bool, error) {
	scope, err := s.userScope(r)
	if err != nil {
		return false, err
	}
	return scope.allows(slug), nil
}

// trackedWorkspaces renders the allowed set for the login page, so it is
// obvious whose coverage an instance holds.
func (s *Server) trackedWorkspaces(r *http.Request) []string {
	set, err := s.allowedWorkspaceSet(r)
	if err != nil {
		return nil
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Server) redirectURI(forge string) string {
	return strings.TrimSuffix(s.baseURL, "/") + "/oauth/" + forge + "/callback"
}

// sanitizeNext confines the post-login redirect to in-site paths, so the
// login URL can never be turned into an open redirect. A safe target is a
// single leading slash whose second character is neither '/' nor '\\' —
// browsers treat "//host" and "/\host" as scheme-relative external URLs.
func sanitizeNext(next string) string {
	if len(next) == 0 || next[0] != '/' {
		return "/"
	}
	if len(next) > 1 && (next[1] == '/' || next[1] == '\\') {
		return "/"
	}
	return next
}

func readStateCookie(r *http.Request) (state, next string) {
	c, err := r.Cookie(stateCookie)
	if err != nil {
		return "", "/"
	}
	state, next, _ = strings.Cut(c.Value, "|")
	return state, sanitizeNext(next)
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// newState returns 256 random bits hex-encoded, used for both the OAuth
// state and session tokens.
func newState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// hashToken is what the sessions table stores instead of the token: a DB
// leak then reveals nothing that authenticates.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
