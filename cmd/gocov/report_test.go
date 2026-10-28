package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
