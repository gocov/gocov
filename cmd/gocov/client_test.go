package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/server"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// TestUploadEndToEnd exercises the CLI upload path against a real server
// instance backed by in-memory stores.
func TestUploadEndToEnd(t *testing.T) {
	st := storemem.New()
	repo := &store.Repo{Slug: "acme/widgets", Token: "tok", DefaultBranch: "main", Forge: "bitbucket"}
	if err := st.CreateRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(server.Config{
		Store:   st,
		Blobs:   blobmem.New(),
		BaseURL: "http://example",
	}))
	defer srv.Close()

	prof := []byte("mode: set\nexample.com/m/a.go:1.1,2.2 4 1\nexample.com/m/a.go:3.1,4.2 1 0\n")

	resp, err := upload(uploadRequest{
		Server:      srv.URL,
		Token:       "tok",
		Format:      "go",
		ProfileData: prof,
		Build:       buildInfo{Repo: "acme/widgets", Commit: "abc", Branch: "main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.TotalPct != 80 || resp.CoveredStmts != 4 || resp.TotalStmts != 5 {
		t.Errorf("resp = %+v, want 80%% 4/5", resp)
	}

	// Ignore patterns ride along and the server reports what they dropped.
	resp, err = upload(uploadRequest{
		Server: srv.URL, Token: "tok", Format: "go",
		ProfileData: []byte("mode: set\nexample.com/m/a.go:1.1,2.2 4 1\nexample.com/m/gen/b.go:1.1,2.2 6 0\n"),
		Ignore:      []string{"gen/**"},
		PathPrefix:  "example.com/m",
		Build:       buildInfo{Repo: "acme/widgets", Commit: "abc2", Branch: "main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.IgnoredFiles != 1 || resp.TotalStmts != 4 || resp.TotalPct != 100 {
		t.Errorf("with ignore: %+v, want 1 ignored file and 4/4 statements", resp)
	}

	// Wrong token surfaces the server error.
	if _, err := upload(uploadRequest{
		Server: srv.URL, Token: "bad", Format: "go", ProfileData: prof,
		Build: buildInfo{Commit: "abc"},
	}); err == nil {
		t.Error("want error with invalid token")
	}
}

// The server's non-fatal notices reach the CI log, right under the
// headline they qualify.
func TestUploadPrintsServerWarnings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 7, "total_pct": 80, "covered_stmts": 4, "total_stmts": 5,
			"build_status": "posted", "code_insights": "posted",
			"warnings": ["diff coverage merged conservatively for 1 changed file(s)"]}`))
	}))
	defer srv.Close()

	resp, err := upload(uploadRequest{
		Server: srv.URL, Token: "tok", Format: "go", ProfileData: []byte("mode: set\n"),
		Build: buildInfo{Commit: "abc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	printResult(&out, resp)
	want := "uploaded: 80.0% (4/5 statements)\n" +
		"warning: diff coverage merged conservatively for 1 changed file(s)\n" +
		"build status: posted\n" +
		"code insights: posted\n"
	if out.String() != want {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}
