package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/auth"
	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// decodeJSON unmarshals a recorder's body, failing the test on bad JSON.
func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return v
}

// postJSON posts to a UI API endpoint the way the app does: a JSON body
// from our own origin, carrying the session cookie. A nil body posts
// nothing, which is what the verb-only endpoints take.
func postJSON(t *testing.T, f *fixture, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

// wantStatus fails the test unless the recorder carries the given status,
// quoting the body so a mismatch says why.
func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, path string, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s: status = %d, want %d (body %s)", path, rec.Code, want, rec.Body)
	}
}

func TestAPISessionOnOpenInstance(t *testing.T) {
	f := newFixture(t, nil)
	rec := get(f, "/api/ui/session")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := decodeJSON[sessionDTO](t, rec)
	if got.AuthEnabled || got.User != nil {
		t.Errorf("open instance session = %+v, want no auth and no user", got)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

// The session endpoint is how the app learns it is signed out, so it must
// answer without a session; every other UI API path is a 401 in JSON, not
// the login redirect a page gets.
func TestAPISignedOut(t *testing.T) {
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)

	rec := get(f, "/api/ui/session")
	if rec.Code != http.StatusOK {
		t.Fatalf("session status = %d, want 200", rec.Code)
	}
	if got := decodeJSON[sessionDTO](t, rec); !got.AuthEnabled || got.User != nil {
		t.Errorf("signed-out session = %+v, want auth enabled and no user", got)
	}

	rec = get(f, "/api/ui/dashboard")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("dashboard status = %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("API answered with a redirect to %q", loc)
	}
}

func TestAPISessionSignedIn(t *testing.T) {
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)
	cookie := signIn(t, f, "/")
	got := decodeJSON[sessionDTO](t, get(f, "/api/ui/session", cookie))
	if got.User == nil || got.User.DisplayName == "" {
		t.Fatalf("signed-in session has no user: %+v", got)
	}
}

// The analytics block is the app's whole PostHog configuration, so what
// it carries is what ends up off-site: the public key and host, and a
// signed-in reader as their numeric gocov id — never an email. With no
// key configured it is absent altogether, which is the "nothing
// off-site" promise self-hosted deployments rely on.
func TestAPISessionAnalytics(t *testing.T) {
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)
	if got := decodeJSON[sessionDTO](t, get(f, "/api/ui/session")); got.Analytics != nil {
		t.Errorf("analytics = %+v with no key configured, want none", got.Analytics)
	}

	f.srv.posthog = PostHog{Key: "phc_abc", Host: "https://eu.i.posthog.com"}
	rec := get(f, "/api/ui/session")
	got := decodeJSON[sessionDTO](t, rec)
	if got.Analytics == nil || got.Analytics.Key != "phc_abc" || got.Analytics.Host != "https://eu.i.posthog.com" {
		t.Fatalf("signed-out analytics = %+v", got.Analytics)
	}
	if got.Analytics.UserID != "" {
		t.Errorf("anonymous reader identified as %q", got.Analytics.UserID)
	}

	sess := signIn(t, f, "/")
	rec = get(f, "/api/ui/session", sess)
	got = decodeJSON[sessionDTO](t, rec)
	users, err := f.store.ListUsers(t.Context())
	if err != nil || len(users) != 1 {
		t.Fatalf("ListUsers = %v, %v; want the one signed-in user", users, err)
	}
	if want := strconv.FormatInt(users[0].ID, 10); got.Analytics.UserID != want {
		t.Errorf("analytics user = %q, want the gocov id %q", got.Analytics.UserID, want)
	}
	// The email is the app's, for the nav chip; PostHog only ever gets the
	// numeric pseudonym.
	if strings.Contains(got.Analytics.UserID, "@") {
		t.Errorf("analytics user %q is not a numeric pseudonym", got.Analytics.UserID)
	}
}

func TestAPIUnknownPathIsJSONNotFound(t *testing.T) {
	f := newFixture(t, nil)
	rec := getAccept(f, "/api/ui/nope", "text/html")
	if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("got %d %q, want a JSON 404", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// Nothing the app reads may carry an upload token. The store's rows hold
// them in the clear, so a handler that marshalled one would hand every
// reader of the page the keys to CI; only the explicit reveal and rotate
// endpoints answer with one, and only to an owner.
func TestAPINeverLeaksTokens(t *testing.T) {
	const (
		wsToken   = "ws-tok-LEAKCANARY-1"
		repoToken = "repo-tok-LEAKCANARY-2"
	)
	ctx := t.Context()
	st := storemem.New()
	ws := &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: wsToken, DefaultBranch: "main"}
	if err := st.CreateWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	repo := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: repoToken, DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	blobs := blobmem.New()
	f := &fixture{
		srv: New(Config{
			Store:   st,
			Blobs:   blobs,
			Parsers: map[string]profile.Parser{"go": profile.GoParser{}},
			BaseURL: "https://gocov.example",
			Auths:   []auth.Provider{&fakeProvider{identity: memberIdentity()}},
		}),
		store: st,
		blobs: blobs,
		repo:  repo,
	}
	seedUpload(t, f)
	sess := signIn(t, f, "/") // an owner of acme: the widest read there is

	for _, path := range []string{
		"/api/ui/session",
		"/api/ui/login?denied=1",
		"/api/ui/dashboard",
		"/api/ui/repos/bitbucket/acme/widgets",
		"/api/ui/uploads/1",
		"/api/ui/uploads/1/files/a.go",
		"/api/ui/workspace-settings/bitbucket/acme",
		"/api/ui/workspace-setup/bitbucket/acme",
		"/api/ui/workspace-setup-status/bitbucket/acme",
		"/api/ui/onboarding",
		"/api/ui/repo-settings/bitbucket/acme/widgets",
	} {
		rec := get(f, path, sess)
		wantStatus(t, rec, "GET "+path, http.StatusOK)
		for _, secret := range []string{wsToken, repoToken, "token_hash", "refresh"} {
			if strings.Contains(rec.Body.String(), secret) {
				t.Errorf("GET %s leaked %q:\n%s", path, secret, rec.Body)
			}
		}
	}

	// The two endpoints that may: an owner asking for the value behind the
	// mask, and a rotation handing back what it just wrote.
	for path, want := range map[string]string{
		"/api/ui/workspace-settings/reveal-token/bitbucket/acme":    wsToken,
		"/api/ui/repo-settings/reveal-token/bitbucket/acme/widgets": repoToken,
	} {
		rec := postJSON(t, f, path, nil, sess)
		wantStatus(t, rec, "POST "+path, http.StatusOK)
		if got := decodeJSON[tokenRevealDTO](t, rec).Token; got != want {
			t.Errorf("POST %s revealed %q, want %q", path, got, want)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("POST %s Cache-Control = %q, want no-store", path, cc)
		}
	}
}

// A cross-site POST riding the session cookie must not reach a handler.
func TestAPIRejectsCrossOriginWrites(t *testing.T) {
	f := newFixture(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/ui/anything", strings.NewReader("{}"))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site POST status = %d, want 403", rec.Code)
	}
}
