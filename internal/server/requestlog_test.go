package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// Every UI API request logs its route pattern, status and duration — the
// pattern, not the path, so a repository's name never reaches the logs.
func TestUIRequestLog(t *testing.T) {
	var buf bytes.Buffer
	srv := New(Config{
		Store:   storemem.New(),
		Blobs:   blobmem.New(),
		BaseURL: "https://gocov.example",
		Logger:  slog.New(slog.NewJSONHandler(&buf, nil)),
	})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ui/repos/github/acme/secret-project", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unknown repo", rec.Code)
	}

	var line map[string]any
	for l := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log line %q: %v", l, err)
		}
		if m["msg"] == "ui request" {
			line = m
		}
	}
	if line == nil {
		t.Fatalf("no ui request line in %q", buf.String())
	}
	if line["route"] != "GET /api/ui/repos/{forge}/{slug...}" {
		t.Errorf("route = %v, want the pattern", line["route"])
	}
	if line["status"] != float64(http.StatusNotFound) {
		t.Errorf("status = %v, want 404", line["status"])
	}
	if _, ok := line["ms"].(float64); !ok {
		t.Errorf("ms missing: %v", line)
	}
	if strings.Contains(buf.String(), "secret-project") {
		t.Errorf("the repository's name reached the log: %s", buf.String())
	}
}

// A handler that writes a body without a status is logged as the 200 the
// server sends.
func TestStatusWriterDefaultsToOK(t *testing.T) {
	sw := &statusWriter{ResponseWriter: httptest.NewRecorder()}
	if sw.status() != http.StatusOK {
		t.Errorf("nothing written: status = %d, want 200", sw.status())
	}
	_, _ = sw.Write([]byte("x"))
	sw.WriteHeader(http.StatusTeapot) // too late: the 200 went with the body
	if sw.status() != http.StatusOK {
		t.Errorf("after a body: status = %d, want 200", sw.status())
	}
}
