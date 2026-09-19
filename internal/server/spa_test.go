package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// /app/… was the app's home while it was built beside the template
// pages. The canonical URLs answer directly now, so the old prefix is a
// permanent redirect to the same path without it — query string and all.
func TestAppPrefixRedirectsToTheCanonicalURL(t *testing.T) {
	f := newFixture(t, nil)
	for path, want := range map[string]string{
		"/app":                             "/",
		"/app/":                            "/",
		"/app/repos/github/acme/widgets":   "/repos/github/acme/widgets",
		"/app/uploads/12?branch=main":      "/uploads/12?branch=main",
		"/app/workspaces/gitlab/grp%2Fsub": "/workspaces/gitlab/grp%2Fsub",
	} {
		rec := get(f, path)
		if rec.Code != http.StatusMovedPermanently {
			t.Errorf("%s: status = %d, want 301", path, rec.Code)
		}
		if got := rec.Header().Get("Location"); got != want {
			t.Errorf("%s -> %q, want %q", path, got, want)
		}
	}
}

// The bundles are content-hashed, so they may be cached forever; the rest
// of the build revalidates. Both answers come from the same middleware,
// so the rule holds whether or not the file is there (a checkout that
// never ran the web build has neither).
func TestAppAssetCaching(t *testing.T) {
	rec := httptest.NewRecorder()
	spaAssetCache(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/static/app/assets/index-deadbeef.js", nil))
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("hashed bundle Cache-Control = %q", cc)
	}
	rec = httptest.NewRecorder()
	spaAssetCache(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/static/app/favicon.svg", nil))
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("unhashed asset Cache-Control = %q", cc)
	}
}
