// Signing in through a forge's OAuth: the login page, the consent
// redirect, and the callback that turns a consent code into a user and a
// session. The state cookie, the redirect URI and the random-token helper
// are shared with the workspace-connect grants in bitbucketgrant.go and
// gitlabgrant.go, which run the same handshake for a different purpose.

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/auth"
	"github.com/gocov/gocov/internal/store"
)

// handleLogin implements GET /login — the sign-in shell. The buttons and
// the denial notice are the app's, read from /api/ui/login; what stays
// here is the answer to "should this visitor be on the sign-in page at
// all", and the status a refused sign-in carries.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if u := s.sessionUser(r); u != nil {
		http.Redirect(w, r, sanitizeNext(r.FormValue("next")), http.StatusFound)
		return
	}
	// A refused sign-in says so in the status too, and only in private
	// mode (M3/D1): a hosted instance never denies one, so ?denied=1 must
	// not be reachable by URL there either.
	status := http.StatusOK
	if r.FormValue("denied") == "1" && !s.hosted {
		status = http.StatusForbidden
	}
	s.serveApp(w, r, status, appHead{Title: "sign in — gocov"})
}

// loginProviders lists the configured providers in the order their
// sign-in buttons render.
func (s *Server) loginProviders() []loginProviderDTO {
	providers := make([]loginProviderDTO, 0, len(s.authOrder))
	for _, p := range s.authOrder {
		providers = append(providers, loginProviderDTO{Name: p.Name(), Label: providerLabel(p.Name())})
	}
	return providers
}

// loginInfoDTO is what the app's sign-in page needs: which providers this
// instance offers, and — only where the page discloses them — the
// workspaces it tracks.
type loginInfoDTO struct {
	Hosted            bool                  `json:"hosted"`
	Providers         []loginProviderDTO    `json:"providers"`
	TrackedWorkspaces []trackedWorkspaceDTO `json:"tracked_workspaces"`
}

type loginProviderDTO struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// trackedWorkspaceDTO is one tracked workspace, its forge named the way
// the store does rather than as a label — the app spells it out itself.
type trackedWorkspaceDTO struct {
	Name  string `json:"name"`
	Forge string `json:"forge"`
}

// handleAPILogin implements GET /api/ui/login. It answers signed-out (it
// is what a signed-out app asks for) and repeats handleLogin's disclosure
// rule: the tracked names tell a denied member whom to ask, but to
// anyone else they say who uses this instance, so they ride along only
// after a real identity was turned away on a private instance.
func (s *Server) handleAPILogin(w http.ResponseWriter, r *http.Request) {
	dto := loginInfoDTO{
		Hosted:            s.hosted,
		Providers:         []loginProviderDTO{},
		TrackedWorkspaces: []trackedWorkspaceDTO{},
	}
	dto.Providers = append(dto.Providers, s.loginProviders()...)
	if r.FormValue("denied") == "1" && !s.hosted {
		for _, k := range s.trackedWorkspaceKeys(r) {
			dto.TrackedWorkspaces = append(dto.TrackedWorkspaces, trackedWorkspaceDTO{Name: k.prefix, Forge: k.forge})
		}
	}
	s.writeJSON(w, dto)
}

// providerLabels maps forge names to their proper spelling; unknown
// forges fall back to their capitalized name.
var providerLabels = map[string]string{
	"bitbucket": "Bitbucket",
	"github":    "GitHub",
	"gitlab":    "GitLab",
}

func providerLabel(name string) string {
	if l, ok := providerLabels[name]; ok {
		return l
	}
	return strings.ToUpper(name[:1]) + name[1:]
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
		Name: stateCookie,
		// Percent-encoded, because a cookie value is not a place for an
		// arbitrary path: net/http drops every byte SetCookie considers
		// invalid — anything non-ASCII, '"', ';' — so an unencoded
		// "/repos/acme/wörk" would come back as "/repos/acme/wrk".
		Value:    state + "|" + url.QueryEscape(sanitizeNext(r.FormValue("next"))),
		Path:     "/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, provider.AuthorizeURL(state, s.redirectURI(provider.Name())), http.StatusFound)
}

// handleOAuthCallback implements GET /oauth/{forge}/callback: the forge
// hands back a consent code here, and this turns it into a signed-in
// session — or into a denial that leaves nothing behind.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := s.oauthProvider(w, r)
	if provider == nil {
		return
	}
	// A returning workspace-connect consent shares this callback URL —
	// the forges enforce an exact redirect-URI match on the configured
	// callback — and is told apart by its own state cookie.
	if g := connectGrantFor(provider.Name()); g != nil && s.connectCallback(g, w, r) {
		return
	}

	code, next, ok := s.verifyCallback(w, r, provider.Name())
	if !ok {
		return
	}
	id, err := provider.Identity(r.Context(), code, s.redirectURI(provider.Name()))
	if err != nil {
		s.log.Error("oauth identity", "forge", provider.Name(), "err", err)
		signInFailed(w, r)
		return
	}
	if !s.admitSignIn(w, r, provider.Name(), id) {
		return
	}

	u, memberships, ok := s.provisionUser(w, r, provider.Name(), id)
	if !ok {
		return
	}
	// A hosted user who belongs to no tracked workspace has nothing to see
	// yet — land them in the onboarding wizard rather than an empty dashboard.
	// Only when no explicit destination was requested: a re-auth aimed at a
	// pending GitHub install (next=/github/setup?...) must be honoured so it
	// can connect after the org snapshot refreshes. Private-mode users always
	// land on the dashboard, which surfaces an onboarding link when empty.
	if s.hosted && memberships == 0 && (next == "" || next == "/") {
		next = "/onboarding"
	}
	if !s.startSession(w, r, u) {
		return
	}
	s.log.Info("sign-in", "forge", provider.Name(), "user", u.DisplayName, "email", u.Email)
	http.Redirect(w, r, next, http.StatusFound)
}

// signInFailed sends the visitor back to the login page. The reason is
// deliberately not in the URL: it is logged for the operator, while the
// browser only learns that sign-in did not work.
func signInFailed(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login?error=1", http.StatusFound)
}

// verifyCallback checks that this callback belongs to a consent this
// browser started: the state cookie must be present and match the state
// the forge echoed back (CSRF/replay protection), and the forge must have
// sent a code rather than an error. It returns the consent code and the
// in-site path to return to afterwards. The state cookie is cleared either
// way — it has served its purpose the moment it is read.
func (s *Server) verifyCallback(w http.ResponseWriter, r *http.Request, forge string) (code, next string, ok bool) {
	state, next := readStateCookie(r)
	clearCookie(w, stateCookie, s.secureCookies)
	code = r.FormValue("code")
	if r.FormValue("error") != "" || code == "" || state == "" || r.FormValue("state") != state {
		s.log.Warn("oauth callback rejected", "forge", forge,
			"forge_error", r.FormValue("error"), "state_ok", r.FormValue("state") == state && state != "")
		signInFailed(w, r)
		return "", "", false
	}
	return code, next, true
}

// admitSignIn applies the membership gate: a private instance only admits
// accounts that belong to a tracked workspace. The gate is private-mode
// only (M3/D1); a hosted instance admits any forge account and routes the
// workspace question to the registration page instead.
func (s *Server) admitSignIn(w http.ResponseWriter, r *http.Request, forge string, id *auth.Identity) bool {
	if s.hosted {
		return true
	}
	allowed, err := s.allowedWorkspaceSet(r)
	if err != nil {
		s.internalError(w, "deriving allowed workspaces", err)
		return false
	}
	for _, ws := range id.Workspaces {
		if admits(allowed, forge, ws) {
			return true
		}
	}
	// No user row, no session (R3): denial must leave nothing behind.
	// Both sides of the failed intersection are logged so an operator
	// can see at a glance whether the fix is a missing registration,
	// a stale GOCOV_ALLOWED_WORKSPACES or a slug mismatch.
	s.log.Warn("sign-in denied", "forge", forge, "account", id.DisplayName, "email", id.Email,
		"member_of", id.Workspaces, "allowed", sortedKeys(allowed))
	http.Redirect(w, r, "/login?denied=1", http.StatusFound)
	return false
}

// provisionUser upserts the account behind the identity and refreshes its
// workspace memberships, returning how many tracked workspaces it belongs
// to. A false last return means the error response is already written.
func (s *Server) provisionUser(w http.ResponseWriter, r *http.Request, forge string, id *auth.Identity) (*store.User, int, bool) {
	u := &store.User{
		Forge:       forge,
		ForgeUUID:   id.ForgeUUID,
		Email:       id.Email,
		DisplayName: id.DisplayName,
		// The workspace snapshot the registration page renders from
		// (M3/D3); refreshed on every login, stale in between.
		ForgeWorkspaces:      id.Workspaces,
		ForgeOwnedWorkspaces: id.OwnedWorkspaces,
	}
	if err := s.store.UpsertUser(r.Context(), u); err != nil {
		s.internalError(w, "provisioning user", err)
		return nil, 0, false
	}
	memberships, err := s.syncMemberships(r.Context(), u)
	if err != nil {
		s.internalError(w, "syncing workspace memberships", err)
		return nil, 0, false
	}
	return u, memberships, true
}

// startSession issues the session: a random token kept only as its hash in
// the database, handed to the browser in an HttpOnly cookie. A false return
// means the error response is already written.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *store.User) bool {
	token, err := newState() // same entropy requirement: 256 random bits
	if err != nil {
		s.internalError(w, "generating session token", err)
		return false
	}
	sess := &store.Session{
		TokenHash: hashToken(token),
		UserID:    u.ID,
		ExpiresAt: time.Now().Add(sessionTTL),
	}
	if err := s.store.CreateSession(r.Context(), sess); err != nil {
		s.internalError(w, "creating session", err)
		return false
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
	return true
}

// syncMemberships persists the user's workspace memberships (M2/D2): the
// tracked workspaces on this forge whose prefix the forge reports the user
// belongs to, each with the role the forge's own role maps to. It runs on
// every sign-in with full-sync semantics, so a membership the forge no
// longer reports is dropped at the next login and a demoted owner
// becomes a member. Matching is per provider (forge + prefix), so two
// forges sharing a prefix stay distinct tenants. Returns how many
// memberships the user ends up with, which decides a hosted user's
// post-login landing page.
func (s *Server) syncMemberships(ctx context.Context, u *store.User) (int, error) {
	tracked, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return 0, err
	}
	var memberships []store.Membership
	for _, ws := range tracked {
		if ws.Forge == u.Forge && slices.Contains(u.ForgeWorkspaces, ws.Prefix) {
			memberships = append(memberships, store.Membership{WorkspaceID: ws.ID, Role: forgeRole(u, ws.Prefix)})
		}
	}
	return len(memberships), s.store.SetUserMemberships(ctx, u.ID, memberships)
}

// forgeRole is the role the user's forge snapshot grants in the workspace
// registered at prefix: owner when the forge says the account administers
// it, member otherwise.
func forgeRole(u *store.User, prefix string) store.Role {
	if slices.Contains(u.ForgeOwnedWorkspaces, prefix) {
		return store.RoleOwner
	}
	return store.RoleMember
}

func (s *Server) redirectURI(forge string) string {
	return strings.TrimSuffix(s.baseURL, "/") + "/oauth/" + forge + "/callback"
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

func readStateCookie(r *http.Request) (state, next string) {
	c, err := r.Cookie(stateCookie)
	if err != nil {
		return "", "/"
	}
	state, escaped, _ := strings.Cut(c.Value, "|")
	next, err = url.QueryUnescape(escaped)
	if err != nil {
		return state, "/"
	}
	return state, sanitizeNext(next)
}

// sanitizeNext confines the post-login redirect to in-site paths, so the
// login URL can never be turned into an open redirect. The target is parsed
// and kept only if it names no host of its own, and what comes back is
// net/url's own serialization rather than the caller's string — so whatever
// survives is a shape this package chose, not one an attacker spelled.
func sanitizeNext(next string) string {
	// The plain forms, refused where a reader looks for them: a target that
	// is not a rooted path at all, and the two scheme-relative spellings
	// that name another origin ("//host", and "/\host" because browsers
	// read '\' as a separator for http(s)). The parse below would catch
	// these too — it finds a Host — but this is the browser rule the whole
	// function exists to respect, so it is worth saying out loud rather
	// than leaving as a consequence of net/url's behaviour.
	if next == "" || next[0] != '/' || strings.HasPrefix(next, "//") || strings.HasPrefix(next, `/\`) {
		return "/"
	}
	// Browsers read '\' as a path separator for http(s) (WHATWG URL,
	// relative slash state) while net/url does not, so fold it before
	// parsing: "/\host" is an authority to the client and a mere path to
	// url.Parse, and "/./\host" is one that http.Redirect's own path.Clean
	// would assemble on the way out.
	folded := strings.ReplaceAll(next, `\`, "/")
	// url.Parse refuses ASCII control bytes, which is exactly what is
	// needed: a browser deletes tab and newline from a URL before resolving
	// it, so "/\t/host" would arrive as "//host", and net/http passes a tab
	// into the header untouched.
	target, err := url.Parse(folded)
	// Host does the real work — Hostname would drop a port, so "//:8080/x"
	// would read as hostless here and as an authority in the browser, and
	// User covers the credentials-only authority "//user@". The Hostname
	// call is nevertheless required: comparing it against a constant is the
	// one URL check CodeQL's go/unvalidated-url-redirection query accepts
	// as clearing target itself (UrlCheck.qll), where the Host comparison
	// only clears the Host read and the taint survives via EscapedPath.
	if err != nil || target.Hostname() != "" || target.Scheme != "" || target.Host != "" || target.User != nil {
		return "/"
	}
	// The same two properties again, on the parsed path this time: an
	// authority-less URL can still be a relative path ("x", "../x") that
	// resolves against wherever the browser happens to be, and a path can
	// still open with the slash pair that starts an authority.
	escaped := target.EscapedPath()
	if !strings.HasPrefix(escaped, "/") || strings.HasPrefix(escaped, "//") || strings.HasPrefix(escaped, `/\`) {
		return "/"
	}
	// Collapse the dot segments here rather than leaving them for
	// http.Redirect, which cleans its target on the way out: twice now a
	// hole has opened in the gap between the string this function approved
	// and the different string that reached the client. Cleaning the
	// escaped form keeps "%2f" an escape rather than a separator, and a
	// cleaned rooted path always begins with exactly one slash, so no
	// authority can appear after this point. Clean is idempotent, so what
	// http.Redirect does next is a no-op.
	out := path.Clean(escaped)
	if strings.HasSuffix(escaped, "/") && !strings.HasSuffix(out, "/") {
		out += "/"
	}
	if target.ForceQuery || target.RawQuery != "" {
		out += "?" + target.RawQuery
	}
	if target.Fragment != "" {
		out += "#" + target.EscapedFragment()
	}
	return out
}
