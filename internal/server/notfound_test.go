package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// getAccept issues a GET carrying the given Accept header, so the catch-all
// can tell a browser navigation from an API client.
func getAccept(f *fixture, path, accept string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", accept)
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func TestNotFoundPage(t *testing.T) {
	f := newFixture(t, nil)

	t.Run("catch-all renders styled page for browser GET", func(t *testing.T) {
		rec := getAccept(f, "/does/not/exist", "text/html")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "404 · not found") {
			t.Errorf("body missing 404 badge:\n%s", body)
		}
		if !strings.Contains(body, "/does/not/exist") {
			t.Errorf("body missing requested path:\n%s", body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("content-type = %q, want text/html", ct)
		}
	})

	t.Run("catch-all stays plain for non-browser GET", func(t *testing.T) {
		rec := getAccept(f, "/does/not/exist", "application/json")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "404 · not found") {
			t.Error("plain 404 should not carry the styled page")
		}
	})

	t.Run("missing repo renders styled page", func(t *testing.T) {
		rec := doGet(t, f, "/repos/acme/ghost")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "404 · not found") {
			t.Errorf("missing repo did not render styled 404:\n%s", rec.Body.String())
		}
	})

	t.Run("missing upload renders styled page", func(t *testing.T) {
		rec := doGet(t, f, "/uploads/9999")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "404 · not found") {
			t.Errorf("missing upload did not render styled 404:\n%s", rec.Body.String())
		}
	})
}
