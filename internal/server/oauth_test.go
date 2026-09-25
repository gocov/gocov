package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/auth"
	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

type fakeProvider struct {
	name            string // "" means "bitbucket"
	identity        *auth.Identity
	err             error
	gotCode         string
	gotRedirectURIs []string // from AuthorizeURL and Identity calls
}

func (f *fakeProvider) Name() string {
	if f.name == "" {
		return "bitbucket"
	}
	return f.name
}

func (f *fakeProvider) AuthorizeURL(state, redirectURI string) string {
	f.gotRedirectURIs = append(f.gotRedirectURIs, redirectURI)
	return "https://" + f.Name() + ".example/authorize?state=" + url.QueryEscape(state) +
		"&redirect_uri=" + url.QueryEscape(redirectURI)
}

func (f *fakeProvider) Identity(_ context.Context, code, redirectURI string) (*auth.Identity, error) {
	f.gotCode = code
	f.gotRedirectURIs = append(f.gotRedirectURIs, redirectURI)
	return f.identity, f.err
}

// memberIdentity is the fixtures' signed-in account: a member of acme and
// personal who administers both on the forge, so it lands as an owner
// wherever those are tracked.
func memberIdentity() *auth.Identity {
	return &auth.Identity{
		ForgeUUID:       "{uuid-1}",
		DisplayName:     "Jane Dev",
		Email:           "jane@example.com",
		Workspaces:      []string{"acme", "personal"},
		OwnedWorkspaces: []string{"acme", "personal"},
	}
}

// plainMemberIdentity is memberIdentity without the admin role on the
// forge: the same memberships, seated as a member everywhere.
func plainMemberIdentity() *auth.Identity {
	id := memberIdentity()
	id.OwnedWorkspaces = nil
	return id
}

// newAuthFixture builds a server with sign-in enabled (unless provider is
// nil) over a store tracking the repo acme/widgets.

func newAuthFixture(t *testing.T, provider auth.Provider, allowed []string) *fixture {
	t.Helper()
	var providers []auth.Provider
	if provider != nil {
		providers = append(providers, provider)
	}
	return newMultiAuthFixture(t, providers, allowed)
}

// newMultiAuthFixture is newAuthFixture for any number of providers.

func newMultiAuthFixture(t *testing.T, providers []auth.Provider, allowed []string) *fixture {
	t.Helper()
	st := storemem.New()
	repo := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "secret-token", DefaultBranch: "main"}
	if err := st.CreateRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	srv := New(Config{
		Store:             st,
		Blobs:             blobmem.New(),
		BaseURL:           "https://gocov.example",
		Auths:             providers,
		AllowedWorkspaces: allowed,
	})
	return &fixture{srv: srv, store: st, repo: repo}
}

func get(f *fixture, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func cookieNamed(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s cookie in response", name)
	return nil
}

// signIn drives the full flow and returns the session cookie.

func signIn(t *testing.T, f *fixture, next string) *http.Cookie {
	t.Helper()
	start := get(f, "/oauth/bitbucket/start?next="+url.QueryEscape(next))
	if start.Code != http.StatusFound {
		t.Fatalf("start: status = %d", start.Code)
	}
	stateCk := cookieNamed(t, start, stateCookie)
	state, _, _ := strings.Cut(stateCk.Value, "|")

	cb := get(f, "/oauth/bitbucket/callback?code=thecode&state="+url.QueryEscape(state), stateCk)
	if cb.Code != http.StatusFound {
		t.Fatalf("callback: status = %d", cb.Code)
	}
	loc := cb.Header().Get("Location")
	assertInSite(t, loc)
	if loc != sanitizeNext(next) {
		t.Fatalf("callback redirected to %q, want %q", loc, sanitizeNext(next))
	}
	return cookieNamed(t, cb, sessionCookie)
}

func TestLoginFlow(t *testing.T) {
	provider := &fakeProvider{identity: memberIdentity()}
	f := newAuthFixture(t, provider, nil)

	sess := signIn(t, f, "/repos/bitbucket/acme/widgets")
	if provider.gotCode != "thecode" {
		t.Errorf("provider got code %q", provider.gotCode)
	}
	if !sess.HttpOnly || sess.SameSite != http.SameSiteLaxMode || !sess.Secure {
		t.Errorf("session cookie flags: %+v", sess)
	}

	// The session works, and the app reads the signed-in identity from it.
	rec := get(f, "/", sess)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated index: status = %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store on protected pages", cc)
	}
	if got := decodeJSON[sessionDTO](t, get(f, "/api/ui/session", sess)); got.User == nil || got.User.DisplayName != "Jane Dev" {
		t.Errorf("session = %+v, want Jane Dev signed in", got.User)
	}

	// JIT provisioning: exactly one user row, refreshed on re-login.
	users, err := f.store.ListUsers(t.Context())
	if err != nil || len(users) != 1 {
		t.Fatalf("users = %v, %v", users, err)
	}
	if users[0].ForgeUUID != "{uuid-1}" || users[0].Email != "jane@example.com" {
		t.Errorf("user = %+v", users[0])
	}
	provider.identity.DisplayName = "Jane Renamed"
	signIn(t, f, "/")
	users, _ = f.store.ListUsers(t.Context())
	if len(users) != 1 || users[0].DisplayName != "Jane Renamed" {
		t.Errorf("second login: users = %+v, want 1 row with refreshed name", users)
	}

	// /login while signed in goes straight back in.
	if rec := get(f, "/login", sess); rec.Code != http.StatusFound {
		t.Errorf("login while signed in: status = %d, want redirect", rec.Code)
	}
}

func TestCallbackRejectsBadState(t *testing.T) {
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)

	start := get(f, "/oauth/bitbucket/start")
	stateCk := cookieNamed(t, start, stateCookie)

	for name, path := range map[string]string{
		"missing state":    "/oauth/bitbucket/callback?code=x",
		"mismatched state": "/oauth/bitbucket/callback?code=x&state=wrong",
		"missing code":     "/oauth/bitbucket/callback?state=" + url.QueryEscape(strings.SplitN(stateCk.Value, "|", 2)[0]),
		"forge error":      "/oauth/bitbucket/callback?error=access_denied",
	} {
		rec := get(f, path, stateCk)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?error=1" {
			t.Errorf("%s: %d -> %q, want redirect to /login?error=1", name, rec.Code, rec.Header().Get("Location"))
		}
	}
	// A callback with no state cookie at all fails too.
	rec := get(f, "/oauth/bitbucket/callback?code=x&state=anything")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?error=1" {
		t.Errorf("no cookie: %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if users, _ := f.store.ListUsers(t.Context()); len(users) != 0 {
		t.Errorf("rejected callbacks must not create users, got %v", users)
	}
}

func TestCallbackProviderFailure(t *testing.T) {
	f := newAuthFixture(t, &fakeProvider{err: errors.New("bitbucket down")}, nil)
	start := get(f, "/oauth/bitbucket/start")
	stateCk := cookieNamed(t, start, stateCookie)
	state, _, _ := strings.Cut(stateCk.Value, "|")

	rec := get(f, "/oauth/bitbucket/callback?code=x&state="+url.QueryEscape(state), stateCk)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?error=1" {
		t.Errorf("%d -> %q, want generic failure redirect", rec.Code, rec.Header().Get("Location"))
	}
}

func TestNonMemberIsDenied(t *testing.T) {
	outsider := &auth.Identity{ForgeUUID: "{uuid-2}", DisplayName: "Mallory", Workspaces: []string{"other"}}
	f := newAuthFixture(t, &fakeProvider{identity: outsider}, nil)

	start := get(f, "/oauth/bitbucket/start")
	stateCk := cookieNamed(t, start, stateCookie)
	state, _, _ := strings.Cut(stateCk.Value, "|")
	rec := get(f, "/oauth/bitbucket/callback?code=x&state="+url.QueryEscape(state), stateCk)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?denied=1" {
		t.Fatalf("%d -> %q, want denial redirect", rec.Code, rec.Header().Get("Location"))
	}

	// No user row, no session.
	if users, _ := f.store.ListUsers(t.Context()); len(users) != 0 {
		t.Errorf("denied login created users: %v", users)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			t.Error("denied login set a session cookie")
		}
	}

	// The sign-in page says so in its status too, so a refused sign-in is
	// not a 200 that merely looks like one.
	denied := get(f, "/login?denied=1")
	if denied.Code != http.StatusForbidden {
		t.Errorf("denial page status = %d, want 403", denied.Code)
	}
	if !strings.Contains(denied.Body.String(), `id="root"`) {
		t.Errorf("denial page is not the app shell:\n%s", denied.Body)
	}
}

// hostileNext is every "next" that must never leave this server pointing at
// another origin. The cases are grouped by the trick they use, because each
// one defeated an earlier version of sanitizeNext.
var hostileNext = []string{
	// The plain forms: an absolute URL, and the scheme-relative spellings.
	"https://evil.example", "//evil.example", `/\evil.example`, "no-slash",
	// An authority needs neither a host nor a scheme to be an authority.
	"//:8080/evil.example", "//user@evil.example", "//@evil.example",
	// A browser deletes every tab and newline from a URL before resolving
	// it, so each of these arrives as "//evil.example".
	"/\t/evil.example", "/\n/evil.example", "/\r/evil.example",
	"/\t\\evil.example", "/pa\tth//evil.example",
	// http.Redirect runs path.Clean on its target, which drops "." and ".."
	// segments while leaving a backslash alone — so these look like paths
	// here and would reach the client collapsed into "/\evil.example".
	`/./\evil.example`, `/./\/evil.example`, `/a/../\evil.example`,
}

// assertInSite fails unless the browser would resolve loc against this
// server. It re-derives the answer the way a URL parser does — remove tab
// and newline, read '\' as a separator — instead of calling sanitizeNext,
// so that a sanitizer bug cannot hide behind an assertion that shares it.
func assertInSite(t *testing.T, loc string) {
	t.Helper()
	asBrowser := strings.NewReplacer("\t", "", "\n", "", "\r", "", `\`, "/").Replace(loc)
	if !strings.HasPrefix(asBrowser, "/") || strings.HasPrefix(asBrowser, "//") {
		t.Errorf("Location %q reads as %q — that is another origin", loc, asBrowser)
	}
}

// Both redirects that carry a caller-supplied next are checked against the
// Location header they actually emit, not against sanitizeNext's return
// value: the bug this test grew out of lived downstream of the sanitizer,
// in http.Redirect's own normalization, where a unit test of the function
// alone is blind. Only the origin is asserted, not the exact landing path —
// a hostile next is neutralized, and where the leftovers point is the
// sanitizer's business, not this test's.
func TestOpenRedirectRejected(t *testing.T) {
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)
	sess := signIn(t, f, "/")
	for _, next := range hostileNext {
		t.Run(next, func(t *testing.T) {
			// GET /login with a live session forwards straight to next.
			assertInSite(t, get(f, "/login?next="+url.QueryEscape(next), sess).Header().Get("Location"))
			// The post-consent redirect takes the same next through the
			// state cookie. (signIn asserts on its Location too.)
			signIn(t, f, next)
		})
	}
}

// In-site paths have to keep working: rejecting a hostile next is only half
// the job, sending every signed-in visitor to "/" would be the other kind of
// bug. What comes back is net/url's serialization, so a path is normalized
// (non-ASCII percent-encoded) but never redirected away from.
func TestSanitizeNextKeepsInSitePaths(t *testing.T) {
	for next, want := range map[string]string{
		"/":                                   "/",
		"/onboarding":                         "/onboarding",
		"/w/bitbucket/acme":                   "/w/bitbucket/acme",
		"/repos/bitbucket/acme/web":           "/repos/bitbucket/acme/web",
		"/repos/bitbucket/acme/web?tab=files": "/repos/bitbucket/acme/web?tab=files",
		"/github/setup?installation_id=42&setup_action=install": "/github/setup?installation_id=42&setup_action=install",
		"/repos/bitbucket/acme/web#L12":                         "/repos/bitbucket/acme/web#L12",
		"/repos/bitbucket/acme/web?q=a+b":                       "/repos/bitbucket/acme/web?q=a+b",
		// Percent-encoded on the way out, and still the same destination.
		"/wörk/spåce": "/w%C3%B6rk/sp%C3%A5ce",
	} {
		if got := sanitizeNext(next); got != want {
			t.Errorf("sanitizeNext(%q) = %q, want %q", next, got, want)
		}
	}
}

// The state cookie carries next across the consent round trip, and
// http.SetCookie deletes every byte it considers invalid — anything
// non-ASCII, '"', ';'. Unencoded, "/repos/bitbucket/acme/wörk" came back as
// "/repos/bitbucket/acme/wrk" and the visitor landed on a 404 after signing in.
func TestNextSurvivesTheStateCookie(t *testing.T) {
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)
	for _, next := range []string{"/repos/bitbucket/acme/wörk", "/a;b", `/a"b`, "/a b", "/repos/bitbucket/acme/web?q=a+b"} {
		t.Run(next, func(t *testing.T) {
			start := get(f, "/oauth/bitbucket/start?next="+url.QueryEscape(next))
			ck := cookieNamed(t, start, stateCookie)
			state, _, _ := strings.Cut(ck.Value, "|")
			cb := get(f, "/oauth/bitbucket/callback?code=thecode&state="+url.QueryEscape(state), ck)
			if got, want := cb.Header().Get("Location"), sanitizeNext(next); got != want {
				t.Errorf("after the round trip Location = %q, want %q", got, want)
			}
		})
	}
}

// What sanitizeNext returns has to be exactly what goes on the wire.
// http.Redirect normalizes its own target, and both open redirects this
// file has had lived in the gap between the string the sanitizer approved
// and the different one the client received; asserting the gap is empty
// retires the whole class rather than the two payloads that found it.
func TestSanitizeNextIsWhatGoesOnTheWire(t *testing.T) {
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)
	sess := signIn(t, f, "/")
	nexts := append([]string{
		"/", "/onboarding", "/repos/bitbucket/acme/web?tab=files", "/repos/bitbucket/acme/web#L12",
		"/wörk/spåce", "/a/b/", "/a/./b", "/a/../b", "/%2f/evil.example",
	}, hostileNext...)
	for _, next := range nexts {
		t.Run(next, func(t *testing.T) {
			loc := get(f, "/login?next="+url.QueryEscape(next), sess).Header().Get("Location")
			if want := sanitizeNext(next); loc != want {
				t.Errorf("Location = %q but sanitizeNext returned %q — http.Redirect rewrote it underneath us", loc, want)
			}
		})
	}
}

// The regression this guards is only visible on the wire: a tab passes
// net/http's header sanitizer (unlike \r and \n, which it rewrites to
// spaces), so "/\t/evil.example" would leave the process intact and reach
// the browser as a scheme-relative URL pointing at another origin.
func TestLoginRedirectHeaderCarriesNoControlBytes(t *testing.T) {
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)
	sess := signIn(t, f, "/")

	rec := get(f, "/login?next="+url.QueryEscape("/\t/evil.example"), sess)

	var raw strings.Builder
	if err := rec.Result().Write(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw.String(), "\t") {
		t.Errorf("response carries a raw tab, which a browser strips out:\n%q", raw.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}
}

func TestTwoProviders(t *testing.T) {
	bb := &fakeProvider{identity: memberIdentity()}
	gh := &fakeProvider{name: "github", identity: &auth.Identity{
		ForgeUUID:   "12345",
		DisplayName: "Hub Dev",
		Email:       "hub@example.com",
		Workspaces:  []string{"acme"},
	}}
	// The fixture tracks bitbucket's acme only, and names are scoped per
	// forge; the operator's explicit list is what lets the GitHub org
	// "acme" in here, so the test stays about provider routing.
	f := newMultiAuthFixture(t, []auth.Provider{bb, gh}, []string{"acme"})

	// The app is offered one button per provider, in the configured order.
	info := decodeJSON[loginInfoDTO](t, get(f, "/api/ui/login"))
	if len(info.Providers) != 2 || info.Providers[0].Name != "bitbucket" || info.Providers[1].Name != "github" {
		t.Errorf("providers = %+v, want bitbucket then github", info.Providers)
	}

	// The GitHub flow signs in through the GitHub provider only, with a
	// forge-specific callback as the redirect URI.
	start := get(f, "/oauth/github/start")
	if start.Code != http.StatusFound {
		t.Fatalf("github start: status = %d", start.Code)
	}
	if loc := start.Header().Get("Location"); !strings.HasPrefix(loc, "https://github.example/authorize?") {
		t.Fatalf("github start redirected to %q", loc)
	}
	stateCk := cookieNamed(t, start, stateCookie)
	state, _, _ := strings.Cut(stateCk.Value, "|")
	cb := get(f, "/oauth/github/callback?code=ghcode&state="+url.QueryEscape(state), stateCk)
	if cb.Code != http.StatusFound || cb.Header().Get("Location") != "/" {
		t.Fatalf("github callback: %d -> %q", cb.Code, cb.Header().Get("Location"))
	}
	if gh.gotCode != "ghcode" || bb.gotCode != "" {
		t.Errorf("codes routed wrong: gh %q, bb %q", gh.gotCode, bb.gotCode)
	}
	for _, uri := range gh.gotRedirectURIs {
		if uri != "https://gocov.example/oauth/github/callback" {
			t.Errorf("github redirect URI = %q", uri)
		}
	}

	// The provisioned user belongs to the github forge.
	users, err := f.store.ListUsers(t.Context())
	if err != nil || len(users) != 1 {
		t.Fatalf("users = %v, %v", users, err)
	}
	if users[0].Forge != "github" || users[0].ForgeUUID != "12345" {
		t.Errorf("user = %+v", users[0])
	}

	// Unknown forges have no login routes.
	if rec := get(f, "/oauth/gitlab/start"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown forge start: status = %d, want 404", rec.Code)
	}
	if rec := get(f, "/oauth/gitlab/callback?code=x&state=y"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown forge callback: status = %d, want 404", rec.Code)
	}
}

func TestLoginPageHidesWorkspacesUntilDenied(t *testing.T) {
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)

	// The plain sign-in page is reachable without any session, and the
	// shell it serves carries nothing about this instance's tenants.
	rec := get(f, "/login")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "acme") {
		t.Errorf("the sign-in shell names a tracked workspace:\n%s", rec.Body)
	}
	// The names the app fills the page with come from the UI API, which
	// discloses them only after a real identity was turned away
	// (TestAPILogin), and the page's own status marks that refusal.
	if rec := get(f, "/login?denied=1"); rec.Code != http.StatusForbidden {
		t.Fatalf("denied status = %d, want 403", rec.Code)
	}
}

// coveredPRDiff touches only a.go lines 2-3, which the profile covers, so
// diff coverage is 100% and no annotations are expected.

// TestTrackedNameOnAnotherForgeAdmitsNobody pins the derived allow-set to
// the forge: the fixture tracks bitbucket's acme/widgets, and a GitHub
// account whose org is also called "acme" is a stranger to it.
func TestTrackedNameOnAnotherForgeAdmitsNobody(t *testing.T) {
	gh := &fakeProvider{name: "github", identity: memberIdentity()}
	f := newAuthFixture(t, gh, nil)

	start := get(f, "/oauth/github/start")
	stateCk := cookieNamed(t, start, stateCookie)
	state, _, _ := strings.Cut(stateCk.Value, "|")
	rec := get(f, "/oauth/github/callback?code=x&state="+url.QueryEscape(state), stateCk)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?denied=1" {
		t.Fatalf("github acme against a bitbucket acme: %d -> %q, want denial", rec.Code, rec.Header().Get("Location"))
	}
	if users, _ := f.store.ListUsers(t.Context()); len(users) != 0 {
		t.Errorf("denied login created users: %v", users)
	}
	// The denial names the forge the workspace is tracked on, so a member
	// of the same-named workspace elsewhere can see the mismatch.
	got := decodeJSON[loginInfoDTO](t, get(f, "/api/ui/login?denied=1"))
	if len(got.TrackedWorkspaces) != 1 || got.TrackedWorkspaces[0].Name != "acme" ||
		got.TrackedWorkspaces[0].Forge != "bitbucket" {
		t.Errorf("tracked workspaces = %+v, want acme on bitbucket", got.TrackedWorkspaces)
	}
}

func TestAPILogin(t *testing.T) {
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, []string{"acme"})

	// The sign-in page answers signed-out: it is how the app offers a way in.
	rec := get(f, "/api/ui/login")
	if rec.Code != http.StatusOK {
		t.Fatalf("login info: status = %d", rec.Code)
	}
	got := decodeJSON[loginInfoDTO](t, rec)
	if len(got.Providers) != 1 || got.Providers[0].Name != "bitbucket" {
		t.Errorf("providers = %+v, want the one configured forge with its label", got.Providers)
	}
	if got.Hosted {
		t.Error("private instance reported as hosted")
	}
	// The tracked names say who uses this instance, so the plain sign-in
	// view must not carry them — only the denial does.
	if len(got.TrackedWorkspaces) != 0 {
		t.Errorf("plain login info discloses tracked workspaces: %+v", got.TrackedWorkspaces)
	}

	denied := decodeJSON[loginInfoDTO](t, get(f, "/api/ui/login?denied=1"))
	if len(denied.TrackedWorkspaces) != 1 || denied.TrackedWorkspaces[0].Name != "acme" {
		t.Errorf("denied login info tracked workspaces = %+v, want acme", denied.TrackedWorkspaces)
	}
}
