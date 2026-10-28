package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	noRetryWait(t)
	env := mapEnv(map[string]string{
		"ACTIONS_ID_TOKEN_REQUEST_URL":   "https://pipelines.actions/token",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN": "req-secret",
	})
	doer := &stubDoer{status: 500, body: "boom"}
	if _, err := mintGitHubOIDC(env, doer, "https://gocov.example"); err == nil {
		t.Fatal("500 response did not error")
	}
}

// seqDoer answers each request with the next of its responses, a zero
// status standing for a network error, and counts the calls.
type seqDoer struct {
	statuses []int
	calls    int
}

func (s *seqDoer) Do(*http.Request) (*http.Response, error) {
	status := s.statuses[min(s.calls, len(s.statuses)-1)]
	s.calls++
	if status == 0 {
		return nil, errors.New("connection reset")
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(`{"value":"the.jwt.token"}`)),
		Header:     make(http.Header),
	}, nil
}

// noRetryWait keeps the id-token retries but drops their waits.
func noRetryWait(t *testing.T) {
	t.Helper()
	saved := oidcRetryDelays
	oidcRetryDelays = make([]time.Duration, len(saved))
	t.Cleanup(func() { oidcRetryDelays = saved })
}

func TestMintGitHubOIDCRetries(t *testing.T) {
	noRetryWait(t)
	env := mapEnv(map[string]string{
		"ACTIONS_ID_TOKEN_REQUEST_URL":   "https://pipelines.actions/token",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN": "req-secret",
	})
	for _, tc := range []struct {
		name      string
		statuses  []int
		wantToken bool
		wantCalls int
	}{
		// GitHub's endpoint answered 504 once on main's CI; a retry is all it took.
		{"a 504 then a token", []int{504, 200}, true, 2},
		{"a dropped connection then a token", []int{0, 200}, true, 2},
		{"rate limited twice, then a token", []int{429, 429, 200}, true, 3},
		{"down every time gives up after three", []int{503}, false, 3},
		{"no connection every time gives up after three", []int{0}, false, 3},
		// A refusal will not change on a retry.
		{"a 403 is final", []int{403, 200}, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doer := &seqDoer{statuses: tc.statuses}
			got, err := mintGitHubOIDC(env, doer, "https://gocov.example")
			if tc.wantToken && (err != nil || got != "the.jwt.token") {
				t.Errorf("got (%q, %v), want the token", got, err)
			}
			if !tc.wantToken && err == nil {
				t.Errorf("got %q and no error, want an error", got)
			}
			if doer.calls != tc.wantCalls {
				t.Errorf("made %d requests, want %d", doer.calls, tc.wantCalls)
			}
		})
	}
}

func TestMintGitHubOIDCBadRequestURL(t *testing.T) {
	// A request URL no request can be built from fails at once: no
	// attempt is made, so nothing is retried.
	env := mapEnv(map[string]string{
		"ACTIONS_ID_TOKEN_REQUEST_URL":   "http://bad host/token",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN": "req-secret",
	})
	doer := &seqDoer{statuses: []int{200}}
	if _, err := mintGitHubOIDC(env, doer, "https://gocov.example"); err == nil {
		t.Error("an unparsable request URL did not error")
	}
	if doer.calls != 0 {
		t.Errorf("made %d requests from an unparsable URL", doer.calls)
	}
}

func mapEnv(m map[string]string) envFunc {
	return func(k string) string { return m[k] }
}

// captured is how an upload authenticated, as newCaptureServer saw it:
// the oidc_token field and the Authorization header.
type captured struct{ oidc, auth string }

// newCaptureServer is an upload endpoint that records the credential each
// request carried and answers every one with a canned receipt.
func newCaptureServer(t *testing.T) (*httptest.Server, *captured) {
	t.Helper()
	got := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		got.oidc = r.FormValue("oidc_token")
		got.auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(uploadResponse{TotalPct: 80, CoveredStmts: 8, TotalStmts: 10, BuildStatus: "posted"})
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// writeProfile puts a one-statement Go profile in a scratch directory and
// returns its path.
func writeProfile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "coverage.out")
	if err := os.WriteFile(path, []byte("mode: set\nexample.com/m/a.go:1.1,2.2 4 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// noGitHubIDToken neutralizes GitHub's id-token env, which a CI job running
// these tests with `id-token: write` really has set — otherwise the CLI's
// GitHub-mint path, which precedes every other secret-less mode, would win
// and mint a real token instead of exercising the path under test.
func noGitHubIDToken(t *testing.T) {
	t.Helper()
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")
}

// TestRunOIDCUpload drives run() end to end: with the id-token env vars set
// and no token, it mints an OIDC token (through the stubbed doer) and sends
// it as the oidc_token field with no bearer header.
func TestRunOIDCUpload(t *testing.T) {
	srv, got := newCaptureServer(t)

	// Swap in a doer that returns a canned JWT for the mint request.
	orig := defaultHTTPDoer
	defaultHTTPDoer = &stubDoer{status: 200, body: `{"value":"minted.jwt.here"}`}
	defer func() { defaultHTTPDoer = orig }()

	profPath := writeProfile(t)
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
	if got.oidc != "minted.jwt.here" {
		t.Errorf("oidc_token field = %q", got.oidc)
	}
	if got.auth != "" {
		t.Errorf("Authorization header set on OIDC upload: %q", got.auth)
	}
}

// On Bitbucket the OIDC token is handed to the step in an env var, so the
// CLI reads it (no mint request) and sends it as oidc_token.
func TestRunBitbucketOIDCUpload(t *testing.T) {
	srv, got := newCaptureServer(t)

	profPath := writeProfile(t)
	t.Setenv("GOCOV_TOKEN", "")
	t.Setenv("GOCOV_SERVER", srv.URL)
	noGitHubIDToken(t)
	// A Bitbucket Pipelines step with oidc: true.
	t.Setenv("BITBUCKET_STEP_OIDC_TOKEN", "bb.jwt.token")
	t.Setenv("BITBUCKET_REPO_FULL_NAME", "acme/widgets")
	t.Setenv("BITBUCKET_COMMIT", "abc123")
	t.Setenv("BITBUCKET_BRANCH", "main")

	if err := run([]string{"upload", profPath}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.oidc != "bb.jwt.token" {
		t.Errorf("oidc_token field = %q", got.oidc)
	}
	if got.auth != "" {
		t.Errorf("Authorization header set on OIDC upload: %q", got.auth)
	}
}

// On GitLab the id_tokens: block hands the job the token in GOCOV_ID_TOKEN,
// which the CLI reads and sends as oidc_token.
func TestRunGitLabOIDCUpload(t *testing.T) {
	srv, got := newCaptureServer(t)

	profPath := writeProfile(t)
	t.Setenv("GOCOV_TOKEN", "")
	t.Setenv("GOCOV_SERVER", srv.URL)
	noGitHubIDToken(t)
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_PROJECT_PATH", "acme/widgets")
	t.Setenv("CI_COMMIT_SHA", "abc123")
	t.Setenv("CI_COMMIT_BRANCH", "main")
	t.Setenv("GOCOV_ID_TOKEN", "gl.jwt.token")

	if err := run([]string{"upload", profPath}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.oidc != "gl.jwt.token" {
		t.Errorf("oidc_token field = %q", got.oidc)
	}
	if got.auth != "" {
		t.Errorf("Authorization header set on OIDC upload: %q", got.auth)
	}
}

// With no token, no id-token permission, and not a fork PR, the upload has
// no credential and stays an error — existing behavior is unchanged.
func TestRunNoCredentialErrors(t *testing.T) {
	profPath := writeProfile(t)
	t.Setenv("GOCOV_TOKEN", "")
	t.Setenv("GOCOV_SERVER", "https://gocov.example")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "push")
	t.Setenv("GITHUB_REPOSITORY", "acme/widgets")
	t.Setenv("GITHUB_SHA", "abc123")
	// No id-token permission and not a pull_request run.
	noGitHubIDToken(t)

	if err := run([]string{"upload", profPath}); err == nil {
		t.Fatal("missing credential did not error")
	}
}

// TestRunOIDCMintFailureSaysSo: with the id-token permission granted but
// GitHub's endpoint failing every try, the error names the failed request
// and does not tell the user to grant a permission they already have.
func TestRunOIDCMintFailureSaysSo(t *testing.T) {
	noRetryWait(t)
	profPath := writeProfile(t)
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	t.Cleanup(tokenEndpoint.Close)
	t.Setenv("GOCOV_TOKEN", "")
	t.Setenv("GOCOV_SERVER", "https://gocov.example")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "push")
	t.Setenv("GITHUB_REPOSITORY", "acme/widgets")
	t.Setenv("GITHUB_SHA", "abc123")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", tokenEndpoint.URL)
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-secret")

	err := run([]string{"upload", profPath})
	if err == nil {
		t.Fatal("a failed OIDC mint with no other credential did not error")
	}
	if msg := err.Error(); !strings.Contains(msg, "returned 504") || strings.Contains(msg, "grant permissions") {
		t.Errorf("error = %q, want the 504 named and no advice to grant the permission", msg)
	}
}
