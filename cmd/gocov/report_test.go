package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/auth"
	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/server"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// reportBody is a read-API answer for a two-part PR commit that left
// lines untested.
const reportBody = `{
  "repo": {"forge": "github", "slug": "acme/widgets"},
  "commit": "7aa8f0162ab5c0ffee", "branch": "feature", "pr_id": "247",
  "parts": 2, "coverage": 88.04, "gate_failed": false,
  "base": {"commit": "c54dc2c0b7d6", "coverage": 87.65}, "delta": 0.39,
  "diff": {"coverage": 82.6, "covered_lines": 19, "total_lines": 23,
    "uncovered": [{"path": "cmd/gocov/oidc.go", "lines": "49-50, 57-58", "spans": [[49, 50], [57, 58]]}],
    "unmatched_files": []},
  "report_url": "https://gocov.example/uploads/1114?parts=merged"
}`

// newReportServer answers the read API with body and records the request.
func newReportServer(t *testing.T, status int, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	got := &http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = *r.Clone(r.Context())
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func noGit(...string) (string, error) { return "", errors.New("no git") }

func TestReportPrintsTheUncoveredChangedLines(t *testing.T) {
	srv, req := newReportServer(t, http.StatusOK, reportBody)
	var out bytes.Buffer
	err := runReport([]string{"-server", srv.URL, "-forge", "github", "-repo", "acme/widgets", "-commit", "7aa8f0162ab5c0ffee"},
		&out, mapEnv(nil), noGit, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/api/v1/reports/github/acme/widgets" || req.URL.Query().Get("commit") != "7aa8f0162ab5c0ffee" {
		t.Errorf("requested %s", req.URL)
	}
	want := `github/acme/widgets @ 7aa8f01 (feature, PR 247)
coverage: 88.0% (+0.4% vs c54dc2c), 2 parts
diff coverage: 82.6% (19/23 changed lines)
uncovered changed lines:
  cmd/gocov/oidc.go: 49-50, 57-58
report: https://gocov.example/uploads/1114?parts=merged
`
	if out.String() != want {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestReportJSONPassesTheAnswerThrough(t *testing.T) {
	srv, _ := newReportServer(t, http.StatusOK, reportBody)
	var out bytes.Buffer
	if err := runReport([]string{"-server", srv.URL, "-forge", "github", "-repo", "acme/widgets", "-commit", "abc", "-json"},
		&out, mapEnv(nil), noGit, http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	if out.String() != reportBody {
		t.Errorf("-json rewrote the body:\n%s", out.String())
	}
}

func TestReportDetectsFromCIAndGit(t *testing.T) {
	srv, req := newReportServer(t, http.StatusOK, reportBody)
	// GitHub Actions names the forge, repo and commit.
	env := mapEnv(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_REPOSITORY": "acme/widgets", "GITHUB_SHA": "abc123"})
	if err := runReport([]string{"-server", srv.URL}, &bytes.Buffer{}, env, noGit, http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/api/v1/reports/github/acme/widgets" || req.URL.Query().Get("commit") != "abc123" {
		t.Errorf("requested %s", req.URL)
	}

	// Outside CI, git's origin remote and HEAD answer the same questions.
	git := func(args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "remote get-url origin":
			return "git@gitlab.com:acme/widgets.git", nil
		case "rev-parse HEAD":
			return "def456", nil
		}
		return "", errors.New("unexpected git " + strings.Join(args, " "))
	}
	if err := runReport([]string{"-server", srv.URL}, &bytes.Buffer{}, mapEnv(nil), git, http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/api/v1/reports/gitlab/acme/widgets" || req.URL.Query().Get("commit") != "def456" {
		t.Errorf("requested %s", req.URL)
	}
}

func TestReportBranchAsksForTheNewestReport(t *testing.T) {
	srv, req := newReportServer(t, http.StatusOK, reportBody)
	if err := runReport([]string{"-server", srv.URL, "-forge", "github", "-repo", "acme/widgets", "-branch", "main"},
		&bytes.Buffer{}, mapEnv(nil), noGit, http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	if q := req.URL.Query(); q.Get("branch") != "main" || q.Has("commit") {
		t.Errorf("query = %s, want branch=main only", req.URL.RawQuery)
	}
}

func TestReportErrors(t *testing.T) {
	missing, _ := newReportServer(t, http.StatusNotFound, `{"error":"not found"}`)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no report yet", []string{"-server", missing.URL, "-forge", "github", "-repo", "acme/widgets", "-commit", "abc"}, "not uploaded yet, or its reports are not public"},
		{"no repo", []string{"-server", missing.URL, "-forge", "github", "-commit", "abc"}, "set -repo"},
		{"no forge", []string{"-server", missing.URL, "-repo", "acme/widgets", "-commit", "abc"}, "set -forge"},
		{"no commit", []string{"-server", missing.URL, "-forge", "github", "-repo", "acme/widgets"}, "set -commit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runReport(tc.args, &bytes.Buffer{}, mapEnv(nil), noGit, http.DefaultClient)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestReportAgainstTheServer runs the command against the real read API
// over an in-memory store: the CLI's decoding and the server's contract
// cannot drift apart without this failing.
func TestReportAgainstTheServer(t *testing.T) {
	st := storemem.New()
	repo := &store.Repo{Forge: "github", Slug: "acme/widgets", Token: "t", DefaultBranch: "main"}
	if err := st.CreateRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertCommitReport(t.Context(), &store.CommitReport{
		RepoID: repo.ID, CommitSHA: "7aa8f0162ab5", Branch: "feature", PRID: "247",
		TotalPct: 88, CoveredStmts: 88, TotalStmts: 100, PartCount: 1, UploadID: 3,
		DiffCoverage: &diffcov.Result{
			Files:        []diffcov.FileCoverage{{Path: "cmd/gocov/oidc.go", CoveredLines: 19, TotalLines: 23, UncoveredLines: []int{49, 50, 57, 58}}},
			CoveredLines: 19, TotalLines: 23,
		},
	}); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(server.New(server.Config{Store: st, Blobs: blobmem.New(), BaseURL: "https://gocov.example"}))
	t.Cleanup(api.Close)

	var out bytes.Buffer
	if err := runReport([]string{"-server", api.URL, "-forge", "github", "-repo", "acme/widgets", "-commit", "7aa8f0162ab5"},
		&out, mapEnv(nil), noGit, http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	want := `github/acme/widgets @ 7aa8f01 (feature, PR 247)
coverage: 88.0%
diff coverage: 82.6% (19/23 changed lines)
uncovered changed lines:
  cmd/gocov/oidc.go: 49-50, 57-58
report: https://gocov.example/uploads/3
`
	if out.String() != want {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestPrintReportShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		rep  reportResponse
		want string
	}{
		{"a branch build with a failed gate", func() reportResponse {
			var r reportResponse
			r.Repo.Forge, r.Repo.Slug = "github", "acme/widgets"
			r.Commit, r.Branch, r.Coverage, r.GateFailed, r.ReportURL = "abcdef123", "main", 61.25, true, "https://g/u/1"
			return r
		}(), "github/acme/widgets @ abcdef1 (main)\ncoverage: 61.2%, gate failed\nreport: https://g/u/1\n"},
		{"a PR that changed no executable lines but untracked files", func() reportResponse {
			var r reportResponse
			r.Repo.Forge, r.Repo.Slug = "gitlab", "grp/sub/app"
			r.Commit, r.Branch, r.PRID, r.Coverage, r.ReportURL = "abc", "docs", "9", 90, "https://g/u/2"
			r.Diff = &struct {
				Coverage     float64 `json:"coverage"`
				CoveredLines int64   `json:"covered_lines"`
				TotalLines   int64   `json:"total_lines"`
				Uncovered    []struct {
					Path  string `json:"path"`
					Lines string `json:"lines"`
				} `json:"uncovered"`
				UnmatchedFiles []string `json:"unmatched_files"`
			}{Coverage: 100, UnmatchedFiles: []string{"gen/a.go", "gen/b.go"}}
			return r
		}(), "gitlab/grp/sub/app @ abc (docs, PR 9)\ncoverage: 90.0%\ndiff coverage: no executable lines changed\n" +
			"changed files without coverage data: gen/a.go, gen/b.go\nreport: https://g/u/2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			printReport(&out, &tc.rep)
			if out.String() != tc.want {
				t.Errorf("output:\n%s\nwant:\n%s", out.String(), tc.want)
			}
		})
	}
}

func TestDetectForge(t *testing.T) {
	remote := func(url string) gitFunc {
		return func(...string) (string, error) { return url, nil }
	}
	for _, tc := range []struct {
		name string
		env  map[string]string
		git  gitFunc
		want string
	}{
		{"Bitbucket Pipelines", map[string]string{"BITBUCKET_REPO_FULL_NAME": "acme/widgets"}, noGit, "bitbucket"},
		{"GitLab CI", map[string]string{"GITLAB_CI": "true"}, noGit, "gitlab"},
		{"a bitbucket.org remote", nil, remote("https://bitbucket.org/acme/widgets.git"), "bitbucket"},
		{"a self-managed GitLab remote", nil, remote("git@git.example.com:acme/widgets.git"), ""},
		{"no remote", nil, noGit, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectForge(mapEnv(tc.env), tc.git); got != tc.want {
				t.Errorf("detectForge = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReportServerFailures(t *testing.T) {
	broken, _ := newReportServer(t, http.StatusBadGateway, "upstream down\n")
	garbled, _ := newReportServer(t, http.StatusOK, "<html>not json</html>")
	args := func(url string, extra ...string) []string {
		return append([]string{"-server", url, "-forge", "github", "-repo", "acme/widgets", "-commit", "abc"}, extra...)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"a 502 names the status and the body", args(broken.URL), "server returned 502: upstream down"},
		{"an answer that is not JSON", args(garbled.URL), "decoding report"},
		{"a stray argument", args(broken.URL, "coverage.out"), "usage: gocov report"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runReport(tc.args, &bytes.Buffer{}, mapEnv(nil), noGit, http.DefaultClient)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestRunDispatchesReport: `gocov report` reaches the command from main's
// entry point, and an unknown command names both.
func TestRunDispatchesReport(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = devNull // run prints to the process's stdout
	t.Cleanup(func() { os.Stdout = stdout; devNull.Close() })
	srv, req := newReportServer(t, http.StatusOK, reportBody)
	if err := run([]string{"report", "-server", srv.URL, "-forge", "github", "-repo", "acme/widgets", "-commit", "abc", "-json"}); err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/api/v1/reports/github/acme/widgets" {
		t.Errorf("requested %s", req.URL)
	}
	if err := run([]string{"reprot"}); err == nil || !strings.Contains(err.Error(), "gocov report") {
		t.Errorf("unknown command: err = %v, want the usage naming gocov report", err)
	}
}

// A private repo is read with what its upload would authenticate with,
// found in the upload's order.
func TestReportSendsTheUploadCredential(t *testing.T) {
	mint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer req-token" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"value":"minted-` + r.URL.Query().Get("audience") + `"}`))
	}))
	t.Cleanup(mint.Close)
	srv, req := newReportServer(t, http.StatusOK, reportBody)
	actions := map[string]string{"ACTIONS_ID_TOKEN_REQUEST_URL": mint.URL, "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "req-token"}

	for _, tc := range []struct {
		name string
		flag []string
		env  map[string]string
		want string
	}{
		{"none: public reports only", nil, nil, ""},
		{"the token", []string{"-token", "tok"}, actions, "Bearer tok"},
		{"GitHub's OIDC token, for this server", nil, actions, "Bearer minted-" + srv.URL},
		{"GitLab's or Bitbucket's", nil, map[string]string{"GOCOV_ID_TOKEN": "a.b.c"}, "Bearer a.b.c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"-server", srv.URL, "-forge", "github", "-repo", "acme/widgets", "-commit", "abc"}, tc.flag...)
			if err := runReport(args, &bytes.Buffer{}, mapEnv(tc.env), noGit, http.DefaultClient); err != nil {
				t.Fatal(err)
			}
			if got := req.Header.Get("Authorization"); got != tc.want {
				t.Errorf("Authorization = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReportNotFoundSaysWhy(t *testing.T) {
	missing, _ := newReportServer(t, http.StatusNotFound, `{"error":"not found"}`)
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(refusing.Close)
	args := []string{"-server", missing.URL, "-forge", "github", "-repo", "acme/widgets", "-commit", "abc"}

	for _, tc := range []struct {
		name string
		flag []string
		env  map[string]string
		want string
	}{
		{"no credential", nil, nil, "set -token or $GOCOV_TOKEN to read a private repo"},
		{"a credential", []string{"-token", "tok"}, nil, "the credential is not for this repo"},
		{"the OIDC mint failed", nil, map[string]string{"ACTIONS_ID_TOKEN_REQUEST_URL": refusing.URL, "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "x"}, "GitHub's OIDC token request failed (id-token endpoint returned 403)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runReport(append(args, tc.flag...), &bytes.Buffer{}, mapEnv(tc.env), noGit, http.DefaultClient)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// signInRequired is a sign-in provider that is never used: its presence
// alone turns the server into one where only the public rule or a
// credential opens a report.
type signInRequired struct{}

func (signInRequired) Name() string                       { return "github" }
func (signInRequired) AuthorizeURL(string, string) string { return "" }
func (signInRequired) Identity(context.Context, string, string) (*auth.Identity, error) {
	return nil, errors.New("unused")
}

// TestReportPrivateRepoAgainstTheServer reads a private repo through the
// real read API: refused without the token, served with it.
func TestReportPrivateRepoAgainstTheServer(t *testing.T) {
	st := storemem.New()
	repo := &store.Repo{Forge: "github", Slug: "acme/secret", Token: "repo-token", DefaultBranch: "main", Visibility: store.VisibilityPrivate}
	if err := st.CreateRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertCommitReport(t.Context(), &store.CommitReport{
		RepoID: repo.ID, CommitSHA: "7aa8f0162ab5", Branch: "main", TotalPct: 91, PartCount: 1, UploadID: 4,
	}); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(server.New(server.Config{
		Store: st, Blobs: blobmem.New(), BaseURL: "https://gocov.example",
		Auths: []auth.Provider{signInRequired{}}, PublicReports: true,
	}))
	t.Cleanup(api.Close)
	args := []string{"-server", api.URL, "-forge", "github", "-repo", "acme/secret", "-commit", "7aa8f0162ab5"}

	if err := runReport(args, &bytes.Buffer{}, mapEnv(nil), noGit, http.DefaultClient); err == nil || !strings.Contains(err.Error(), "not public") {
		t.Errorf("without the token: err = %v", err)
	}
	var out bytes.Buffer
	if err := runReport(append(args, "-token", "repo-token"), &out, mapEnv(nil), noGit, http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "github/acme/secret @ 7aa8f01 (main)\ncoverage: 91.0%\n") {
		t.Errorf("output:\n%s", out.String())
	}
}

// The request never reaching a server is said, whether the URL cannot be
// built or nothing answers at it.
func TestReportUnreachableServer(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	for _, server := range []string{"http://bad host", gone.URL} {
		err := runReport([]string{"-server", server, "-forge", "github", "-repo", "acme/widgets", "-commit", "abc"},
			&bytes.Buffer{}, mapEnv(nil), noGit, http.DefaultClient)
		if err == nil {
			t.Errorf("-server %s: no error", server)
		}
	}
}
