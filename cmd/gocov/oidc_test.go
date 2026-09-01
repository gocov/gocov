package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubDoer is an httpDoer that captures the request and returns a canned
// response.
type stubDoer struct {
	req    *http.Request
	status int
	body   string
	err    error
}

func (s *stubDoer) Do(req *http.Request) (*http.Response, error) {
	s.req = req
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

func TestMintGitHubOIDC(t *testing.T) {
	env := mapEnv(map[string]string{
		"ACTIONS_ID_TOKEN_REQUEST_URL":   "https://pipelines.actions/token?api=2",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN": "req-secret",
	})
	doer := &stubDoer{status: 200, body: `{"value":"the.jwt.token","count":1}`}

	got, err := mintGitHubOIDC(env, doer, "https://gocov.example/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "the.jwt.token" {
		t.Fatalf("token = %q", got)
	}
	// The audience is appended (server URL, trailing slash trimmed) and the
	// request token rides the Authorization header, never the URL.
	if q := doer.req.URL.Query().Get("audience"); q != "https://gocov.example" {
		t.Errorf("audience = %q, want https://gocov.example", q)
	}
	if doer.req.URL.Query().Get("api") != "2" {
		t.Errorf("existing query param dropped: %s", doer.req.URL.RawQuery)
	}
	if h := doer.req.Header.Get("Authorization"); h != "Bearer req-secret" {
		t.Errorf("auth header = %q", h)
	}
}

func TestMintGitHubOIDCUnavailable(t *testing.T) {
	// No id-token env vars (no permission / not Actions): not an error, just
	// nothing to mint.
	doer := &stubDoer{status: 200, body: `{"value":"x"}`}
	got, err := mintGitHubOIDC(mapEnv(nil), doer, "https://gocov.example")
	if err != nil || got != "" {
		t.Fatalf("got (%q, %v), want empty and nil", got, err)
	}
	if doer.req != nil {
		t.Error("made a request without the id-token env vars")
	}
}

func TestMintGitHubOIDCServerError(t *testing.T) {
	env := mapEnv(map[string]string{
		"ACTIONS_ID_TOKEN_REQUEST_URL":   "https://pipelines.actions/token",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN": "req-secret",
	})
	doer := &stubDoer{status: 500, body: "boom"}
	if _, err := mintGitHubOIDC(env, doer, "https://gocov.example"); err == nil {
		t.Fatal("500 response did not error")
	}
}

func mapEnv(m map[string]string) envFunc {
	return func(k string) string { return m[k] }
}

// TestRunOIDCUpload drives run() end to end: with the id-token env vars set
// and no token, it mints an OIDC token (through the stubbed doer) and sends
// it as the oidc_token field with no bearer header.
func TestRunOIDCUpload(t *testing.T) {
	var gotOIDC, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		gotOIDC = r.FormValue("oidc_token")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(uploadResponse{TotalPct: 80, CoveredStmts: 8, TotalStmts: 10, BuildStatus: "posted"})
	}))
	defer srv.Close()

	// Swap in a doer that returns a canned JWT for the mint request.
	orig := defaultHTTPDoer
	defaultHTTPDoer = &stubDoer{status: 200, body: `{"value":"minted.jwt.here"}`}
	defer func() { defaultHTTPDoer = orig }()

	dir := t.TempDir()
	profPath := filepath.Join(dir, "coverage.out")
	if err := os.WriteFile(profPath, []byte("mode: set\nexample.com/m/a.go:1.1,2.2 4 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOCOV_TOKEN", "")
	t.Setenv("GOCOV_SERVER", srv.URL)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "acme/widgets")
	t.Setenv("GITHUB_SHA", "abc123")
	t.Setenv("GITHUB_REF_NAME", "main")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://pipelines.actions/token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-secret")

	if err := run([]string{"upload", profPath}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotOIDC != "minted.jwt.here" {
		t.Errorf("oidc_token field = %q", gotOIDC)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header set on OIDC upload: %q", gotAuth)
	}
}

// With no token, no id-token permission, and not a fork PR, the upload has
// no credential and stays an error — existing behavior is unchanged.
func TestRunNoCredentialErrors(t *testing.T) {
	dir := t.TempDir()
	profPath := filepath.Join(dir, "coverage.out")
	if err := os.WriteFile(profPath, []byte("mode: set\nexample.com/m/a.go:1.1,2.2 4 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOCOV_TOKEN", "")
	t.Setenv("GOCOV_SERVER", "https://gocov.example")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "push")
	t.Setenv("GITHUB_REPOSITORY", "acme/widgets")
	t.Setenv("GITHUB_SHA", "abc123")
	// No ACTIONS_ID_TOKEN_REQUEST_* and not a pull_request run.
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")

	if err := run([]string{"upload", profPath}); err == nil {
		t.Fatal("missing credential did not error")
	}
}
