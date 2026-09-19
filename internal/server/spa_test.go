package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
