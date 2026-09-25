package server

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	forgefake "github.com/gocov/gocov/internal/forge/fake"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

const testProfile = `mode: set
example.com/m/a.go:1.1,5.2 6 1
example.com/m/a.go:7.1,9.2 2 0
example.com/m/b.go:1.1,3.2 2 1
`

// testProfile: a.go 6/8, b.go 2/2, total 8/10 = 80%.

type fixture struct {
	srv   *Server
	store *storemem.Store
	blobs *blobmem.Store
	forge *forgefake.Forge
	repo  *store.Repo
}

// newFixture builds a bitbucket-forge server with the repo acme/widgets.
// A non-nil connected map wires a one-click Bitbucket connection (grant
// on workspace acme) so the repo's forge surfaces are reachable through
// f.forge; nil leaves the repo with no forge access. The map's contents
// are ignored — only nil vs non-nil matters — so existing call sites that
// passed credential maps keep working.

func newFixture(t *testing.T, connected map[string]string) *fixture {
	t.Helper()
	ctx := t.Context()
	st := storemem.New()
	repo := &store.Repo{
		Forge:         "bitbucket",
		Slug:          "acme/widgets",
		Token:         "secret-token",
		DefaultBranch: "main",
	}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	blobs := blobmem.New()
	ff := forgefake.New()
	cfg := Config{
		Store: st,
		Blobs: blobs,
		Parsers: map[string]profile.Parser{
			"go":        profile.GoParser{},
			"lcov":      profile.LCOVParser{},
			"jacoco":    profile.JaCoCoParser{},
			"cobertura": profile.CoberturaParser{},
			"clover":    profile.CloverParser{},
			"simplecov": profile.SimpleCovParser{},
		},
		BaseURL: "https://gocov.example",
	}
	if connected != nil {
		ws := &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "ws-secret", DefaultBranch: "main"}
		if err := st.CreateWorkspace(ctx, ws); err != nil {
			t.Fatal(err)
		}
		if err := st.SetWorkspaceGrant(ctx, ws.ID, "bitbucket", store.Grant{Account: "covbot", RefreshToken: "rt-0"}); err != nil {
			t.Fatal(err)
		}
		cfg.BitbucketConnect = &fakeBBConnect{grantForge: ff}
	}
	return &fixture{srv: New(cfg), store: st, blobs: blobs, forge: ff, repo: repo}
}

func multipartUpload(t *testing.T, fields map[string]string, profileBody string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if profileBody != "" {
		fw, err := mw.CreateFormFile("profile", "coverage.out")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, profileBody); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func doUpload(t *testing.T, f *fixture, token string, fields map[string]string, profileBody string) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := multipartUpload(t, fields, profileBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", body)
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func getAccept(f *fixture, path, accept string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", accept)
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	get := func(srv *Server) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		return rec
	}
	base := Config{
		Store:   storemem.New(),
		Blobs:   blobmem.New(),
		Parsers: map[string]profile.Parser{"go": profile.GoParser{}},
	}

	t.Run("no probe configured", func(t *testing.T) {
		if rec := get(New(base)); rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
	t.Run("healthy probe", func(t *testing.T) {
		cfg := base
		cfg.Health = func(context.Context) error { return nil }
		if rec := get(New(cfg)); rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
	t.Run("failing probe", func(t *testing.T) {
		cfg := base
		cfg.Health = func(context.Context) error { return errFake }
		if rec := get(New(cfg)); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rec.Code)
		}
	})
}

// Every page route answers with the app shell: Go settles the access
// question and the status, React draws the body. The shell is the same
// document on all of them — only the head and the status differ. (/login
// is the exception this fixture cannot show: with no sign-in configured
// it redirects to the dashboard, which oauth_test.go covers.)
func TestPageRoutesServeTheShell(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "abc123def456789", "branch": "main"}, testProfile)

	for _, path := range []string{
		"/",
		"/onboarding",
		"/_components",
		"/w/bitbucket/acme",
		"/workspace-settings/bitbucket/acme",
		"/workspace-setup/bitbucket/acme",
		"/repo-settings/bitbucket/acme/widgets",
		"/repos/bitbucket/acme/widgets",
		"/uploads/1",
		"/uploads/1/files/example.com/m/a.go",
	} {
		rec := get(f, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: content-type = %q, want text/html", path, ct)
		}
		if !strings.Contains(rec.Body.String(), `id="root"`) {
			t.Errorf("%s did not serve the app shell:\n%s", path, rec.Body)
		}
	}

	// A report that does not exist is a 404 — still the shell, so the app
	// draws its not-found panel.
	for _, path := range []string{"/repos/bitbucket/no/such", "/uploads/999"} {
		rec := get(f, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `id="root"`) {
			t.Errorf("%s: 404 did not serve the shell:\n%s", path, rec.Body)
		}
	}
}

// The shell is servable from a binary built without the web bundle, so
// every status code, redirect and head injection is testable with the Go
// toolchain alone.
func TestShellFallbackWithoutTheWebBuild(t *testing.T) {
	shell := string(injectHead([]byte(fallbackShell), appHead{}))
	for _, want := range []string{"<!doctype html>", shellTitle, `<div id="root"></div>`, "not built into this binary"} {
		if !strings.Contains(shell, want) {
			t.Errorf("the built-in shell misses %q:\n%s", want, shell)
		}
	}
	// Whichever shell the binary carries, head injection has an anchor to
	// replace and the app has a root to mount on.
	built := string(appShell())
	if !strings.Contains(built, shellTitle) || !strings.Contains(built, `id="root"`) {
		t.Errorf("the served shell broke its contract:\n%s", built)
	}
}

// Head injection replaces exactly the one title tag, and escapes every
// dynamic value on the way in: a slug is attacker-chosen (anyone can name
// a repo), so it must never be able to close a tag.
func TestShellHeadInjectionEscapes(t *testing.T) {
	const shell = "<head>" + shellTitle + "</head><body><div id=\"root\"></div></body>"
	got := string(injectHead([]byte(shell), appHead{
		Title: `acme/<script>alert(1)</script> code coverage — gocov`,
		Extra: `<link rel="canonical" href="https://gocov.example/repos/github/acme/x">`,
	}))
	if strings.Contains(got, "<script>") {
		t.Errorf("a slug broke out of the title tag:\n%s", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("the title was not escaped:\n%s", got)
	}
	if !strings.Contains(got, `<link rel="canonical"`) {
		t.Errorf("the extra head tags were dropped:\n%s", got)
	}
	if strings.Contains(got, shellTitle) {
		t.Errorf("the anchor tag survived the replacement:\n%s", got)
	}
	// Nothing to inject leaves the shell byte for byte as it was.
	if got := string(injectHead([]byte(shell), appHead{})); got != shell {
		t.Errorf("an empty head rewrote the shell:\n%s", got)
	}
}

func TestStaticAssetsServed(t *testing.T) {
	f := newFixture(t, nil)
	rec := get(f, "/static/favicon.svg")
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Errorf("favicon: code=%d len=%d", rec.Code, rec.Body.Len())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Errorf("favicon: no cache header (%q)", cc)
	}
}

func TestNotFoundPage(t *testing.T) {
	f := newFixture(t, nil)

	t.Run("catch-all serves the shell for a browser GET", func(t *testing.T) {
		rec := getAccept(f, "/does/not/exist", "text/html")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `id="root"`) {
			t.Errorf("body is not the shell:\n%s", rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("content-type = %q, want text/html", ct)
		}
		// The requested path is never echoed back: a 404 must read the same
		// for a mistyped URL and for a repo the viewer may not see (D3).
		if strings.Contains(rec.Body.String(), "/does/not/exist") {
			t.Errorf("the 404 echoes the requested path:\n%s", rec.Body)
		}
	})

	t.Run("catch-all stays plain for non-browser GET", func(t *testing.T) {
		rec := getAccept(f, "/does/not/exist", "application/json")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if strings.Contains(rec.Body.String(), `id="root"`) {
			t.Error("a plain 404 should not carry the shell")
		}
	})
}
