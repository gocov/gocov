package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/forge"
	forgefake "github.com/gocov/gocov/internal/forge/fake"
	"github.com/gocov/gocov/internal/forge/github"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/server"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

func TestDetectGitHubRun(t *testing.T) {
	event := `{"pull_request": {"head": {"repo": {"full_name": "forker/widgets"}}}}`
	tests := []struct {
		name  string
		env   map[string]string
		files map[string]string
		want  runInfo
	}{
		{
			name: "pull_request run",
			env: map[string]string{
				"GITHUB_ACTIONS":     "true",
				"GITHUB_EVENT_NAME":  "pull_request",
				"GITHUB_RUN_ID":      "9001",
				"GITHUB_RUN_ATTEMPT": "2",
				"GITHUB_EVENT_PATH":  "/event.json",
			},
			files: map[string]string{"/event.json": event},
			want:  runInfo{EventName: "pull_request", RunID: "9001", RunAttempt: "2", HeadRepo: "forker/widgets"},
		},
		{
			name: "attempt defaults to 1",
			env: map[string]string{
				"GITHUB_ACTIONS":    "true",
				"GITHUB_EVENT_NAME": "pull_request",
				"GITHUB_RUN_ID":     "9001",
			},
			want: runInfo{EventName: "pull_request", RunID: "9001", RunAttempt: "1"},
		},
		{
			name: "outside actions",
			env:  map[string]string{"GITHUB_RUN_ID": "9001"},
			want: runInfo{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := func(k string) string { return tt.env[k] }
			readFile := func(path string) ([]byte, error) {
				if c, ok := tt.files[path]; ok {
					return []byte(c), nil
				}
				return nil, os.ErrNotExist
			}
			if got := detectGitHubRun(env, readGitHubEvent(env, readFile)); got != tt.want {
				t.Errorf("detectGitHubRun = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTokenlessEligible(t *testing.T) {
	if (runInfo{EventName: "push", RunID: "1"}).tokenlessEligible() {
		t.Error("push run eligible")
	}
	if (runInfo{EventName: "pull_request"}).tokenlessEligible() {
		t.Error("run without id eligible")
	}
	if !(runInfo{EventName: "pull_request", RunID: "1"}).tokenlessEligible() {
		t.Error("pull_request run not eligible")
	}
}

// fakeApp is a server.GitHubApp accepting every tokenless claim.
type fakeApp struct{ fg forge.Forge }

func (f fakeApp) ForgeClient(context.Context, int64) (forge.Forge, error) { return f.fg, nil }
func (fakeApp) InstallationAccount(context.Context, int64) (string, error) {
	return "acme", nil
}
func (fakeApp) InstallURL(context.Context) (string, error) { return "", nil }
func (fakeApp) VerifyRunClaim(context.Context, int64, github.RunClaim) error {
	return nil
}

// newTokenlessServer is a live server whose acme workspace is connected
// to a claim-accepting App installation.
func newTokenlessServer(t *testing.T) *httptest.Server {
	t.Helper()
	ctx := t.Context()
	st := storemem.New()
	repo := &store.Repo{Slug: "acme/widgets", Token: "tok", DefaultBranch: "main", Forge: "github"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	ws := &store.Workspace{Forge: "github", Prefix: "acme", Token: "ws-tok", DefaultBranch: "main", GitHubInstallationID: 77}
	if err := st.CreateWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(server.Config{
		Store:     st,
		Blobs:     blobmem.New(),
		Parsers:   map[string]profile.Parser{"go": profile.GoParser{}},
		BaseURL:   "http://example",
		GitHubApp: fakeApp{fg: forgefake.New()},
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTokenlessUploadEndToEnd exercises the CLI's tokenless request shape
// against a real server: no Authorization header, the workflow-run claim
// as form fields.
func TestTokenlessUploadEndToEnd(t *testing.T) {
	srv := newTokenlessServer(t)
	prof := []byte("mode: set\nexample.com/m/a.go:1.1,2.2 4 1\nexample.com/m/a.go:3.1,4.2 1 0\n")

	resp, err := upload(uploadRequest{
		Server:      srv.URL,
		Format:      "go",
		ProfileData: prof,
		Build:       buildInfo{Repo: "acme/widgets", Commit: "abc123", Branch: "feature", PRID: "42"},
		Run:         runInfo{EventName: "pull_request", RunID: "9001", RunAttempt: "1", HeadRepo: "forker/widgets"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.TotalPct != 80 {
		t.Errorf("resp = %+v, want 80%%", resp)
	}

	// The same claim replayed is refused — and surfaces as a serverError,
	// which tokenless mode turns into a log line and exit 0.
	_, err = upload(uploadRequest{
		Server:      srv.URL,
		Format:      "go",
		ProfileData: prof,
		Build:       buildInfo{Repo: "acme/widgets", Commit: "abc123", Branch: "feature", PRID: "42"},
		Run:         runInfo{EventName: "pull_request", RunID: "9001", RunAttempt: "1", HeadRepo: "forker/widgets"},
	})
	if err == nil {
		t.Fatal("replayed claim accepted")
	}
	if srvErr, ok := errors.AsType[*serverError](err); !ok || srvErr.code != 409 {
		t.Errorf("replay error = %#v, want a 409 serverError", err)
	}
}

// A tokenless run whose upload is refused must not fail the build: run()
// prints the reason and returns nil.
func TestRunTokenlessRejectionExitsZero(t *testing.T) {
	srv := newTokenlessServer(t)

	profPath := writeProfile(t)
	eventPath := filepath.Join(t.TempDir(), "event.json")
	event := `{"pull_request": {"number": 42, "head": {"sha": "abc123", "ref": "feature", "repo": {"full_name": "forker/widgets"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOCOV_TOKEN", "")
	t.Setenv("GOCOV_SERVER", srv.URL)
	noGitHubIDToken(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_RUN_ID", "9001")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	t.Setenv("GITHUB_REPOSITORY", "acme/unknown") // not tracked: the server refuses

	if err := run([]string{"upload", profPath}); err != nil {
		t.Fatalf("tokenless rejection failed the build: %v", err)
	}

	// The same refusal with a token stays a hard error.
	t.Setenv("GOCOV_TOKEN", "wrong")
	if err := run([]string{"upload", profPath}); err == nil {
		t.Fatal("token-authenticated rejection exited zero")
	}
}
