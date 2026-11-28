package main

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/server"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// TestMain runs the tests outside any CI. The tests that drive run() read
// the real environment, and on a CI runner its own variables leak in: a
// GitHub runner's GITHUB_ACTIONS outranks the Bitbucket and GitLab
// variables a test sets, and its GITHUB_EVENT_PATH swaps in the PR's head
// SHA, which the runner's shallow merge checkout cannot show. Which paths
// the tests took — and so the CLI's coverage — depended on the event that
// ran CI. Each test sets the variables it means.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		for _, prefix := range []string{"GITHUB_", "ACTIONS_", "BITBUCKET_", "GITLAB_", "CI_", "GOCOV_"} {
			if strings.HasPrefix(name, prefix) {
				os.Unsetenv(name)
			}
		}
	}
	os.Exit(m.Run())
}

// newGatedServer is a live server tracking acme/widgets, whose gate
// requires 90% total coverage.
func newGatedServer(t *testing.T) *httptest.Server {
	t.Helper()
	st := storemem.New()
	repo := &store.Repo{Slug: "acme/widgets", Token: "tok", DefaultBranch: "main", Forge: "bitbucket",
		Gate: store.Gate{MinCoverage: new(90.0)}}
	if err := st.CreateRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(server.Config{Store: st, Blobs: blobmem.New(), BaseURL: "http://example"}))
	t.Cleanup(srv.Close)
	return srv
}

// TestUploadExitStatus pins the exit status a CI wrapper reads: a failed
// gate under -fail-on-gate has its own, and the flag changes nothing else —
// not a passing gate, not an upload error, not the secret-less rule that a
// refused upload never fails the build.
func TestUploadExitStatus(t *testing.T) {
	const (
		below = "mode: set\nexample.com/m/a.go:1.1,2.2 4 1\nexample.com/m/a.go:3.1,4.2 1 0\n" // 80%
		above = "mode: set\nexample.com/m/a.go:1.1,2.2 4 1\n"                                 // 100%
	)
	tests := []struct {
		name       string
		failOnGate bool
		token      string
		oidc       string // a Bitbucket step's OIDC token, for the secret-less path
		profile    string
		serverDown bool
		want       int
		// wantErr starts the line main prints after "gocov: ".
		wantErr string
	}{
		{name: "gate failed with -fail-on-gate", failOnGate: true, token: "tok", profile: below,
			want: exitGateFailed, wantErr: "coverage gate failed: "},
		{name: "gate failed without the flag", token: "tok", profile: below, want: 0},
		{name: "gate passed with -fail-on-gate", failOnGate: true, token: "tok", profile: above, want: 0},
		{name: "rejected token with -fail-on-gate", failOnGate: true, token: "wrong", profile: below,
			want: 1, wantErr: "server returned 401"},
		{name: "unreachable server with -fail-on-gate", failOnGate: true, token: "tok", profile: below, serverDown: true,
			want: 1},
		{name: "unreadable profile with -fail-on-gate", failOnGate: true, token: "tok", profile: "not a coverage profile\n",
			want: 1, wantErr: "could not detect the coverage format"},
		{name: "refused secret-less upload with -fail-on-gate", failOnGate: true, oidc: "bb.jwt.token", profile: below,
			want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newGatedServer(t)
			if tt.serverDown {
				srv.Close()
			}
			noGitHubIDToken(t)
			t.Setenv("BITBUCKET_STEP_OIDC_TOKEN", tt.oidc)

			args := []string{"upload", "-server", srv.URL, "-token", tt.token,
				"-repo", "acme/widgets", "-commit", "abc123", "-branch", "main"}
			if tt.failOnGate {
				args = append(args, "-fail-on-gate")
			}
			err := run(append(args, writeProfileOf(t, tt.profile)))
			if got := exitStatus(err); got != tt.want {
				t.Errorf("exit status = %d (run: %v), want %d", got, err, tt.want)
			}
			if tt.wantErr != "" && (err == nil || !strings.HasPrefix(err.Error(), tt.wantErr)) {
				t.Errorf("run error = %v, want it to start with %q", err, tt.wantErr)
			}
		})
	}
}
