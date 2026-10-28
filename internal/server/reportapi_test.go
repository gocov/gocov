package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/store"
)

// seedCommitReport stores a merged report the way the upload pipeline
// would have left one.
func seedCommitReport(t *testing.T, f *fixture, cr *store.CommitReport) {
	t.Helper()
	cr.RepoID = f.repo.ID
	if cr.CreatedAt.IsZero() {
		cr.CreatedAt = time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
		cr.UpdatedAt = cr.CreatedAt
	}
	if err := f.store.UpsertCommitReport(t.Context(), cr); err != nil {
		t.Fatal(err)
	}
}

// prReport is a two-part PR commit whose diff left lines untested in one
// file and none in another.
func prReport() *store.CommitReport {
	return &store.CommitReport{
		CommitSHA: "feed1234", Branch: "feature", PRID: "42",
		TotalPct: 80, CoveredStmts: 80, TotalStmts: 100,
		PartCount: 2, UploadID: 7,
		DiffCoverage: &diffcov.Result{
			Files: []diffcov.FileCoverage{
				{Path: "cmd/gocov/oidc.go", CoveredLines: 19, TotalLines: 23, UncoveredLines: []int{49, 50, 57, 58}},
				{Path: "cmd/gocov/main.go", CoveredLines: 5, TotalLines: 5},
			},
			CoveredLines: 24, TotalLines: 28,
			UnmatchedFiles: []string{"docs/cli.md"},
		},
	}
}

func TestReportAPICommit(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	seedCommitReport(t, f, prReport())

	rec := get(f, "/api/v1/reports/bitbucket/acme/widgets?commit=feed1234")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	got := decodeJSON[reportAPIDTO](t, rec)
	if got.Commit != "feed1234" || got.Branch != "feature" || got.PRID != "42" || got.Parts != 2 {
		t.Errorf("head = %+v", got)
	}
	if got.Coverage != 80 || got.CoveredStatements != 80 || got.TotalStatements != 100 {
		t.Errorf("totals = %v %d/%d", got.Coverage, got.CoveredStatements, got.TotalStatements)
	}
	// Two parts: the link is the merged page, as on the forge surfaces.
	if want := "https://gocov.example/uploads/7?parts=merged"; got.ReportURL != want {
		t.Errorf("report_url = %q, want %q", got.ReportURL, want)
	}
	if got.Diff == nil {
		t.Fatal("diff is null")
	}
	// Only the file with untested changed lines is listed, both spellings.
	want := []reportAPIFileDTO{{Path: "cmd/gocov/oidc.go", Lines: "49-50, 57-58", Spans: [][2]int{{49, 50}, {57, 58}}}}
	if !reflect.DeepEqual(got.Diff.Uncovered, want) {
		t.Errorf("uncovered = %+v, want %+v", got.Diff.Uncovered, want)
	}
	if got.Diff.CoveredLines != 24 || got.Diff.TotalLines != 28 || !reflect.DeepEqual(got.Diff.UnmatchedFiles, []string{"docs/cli.md"}) {
		t.Errorf("diff = %+v", got.Diff)
	}
	// Nothing earlier to compare with.
	if got.Base != nil || got.Delta != nil {
		t.Errorf("base = %+v, delta = %v, want both null", got.Base, got.Delta)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=60" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestReportAPIBranchAndBaseline(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	earlier := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	seedCommitReport(t, f, &store.CommitReport{CommitSHA: "old1", Branch: "main", TotalPct: 70, PartCount: 1, UploadID: 1, CreatedAt: earlier, UpdatedAt: earlier})
	seedCommitReport(t, f, &store.CommitReport{CommitSHA: "new2", Branch: "main", TotalPct: 75, PartCount: 1, UploadID: 2})
	seedCommitReport(t, f, prReport())

	// No commit, no branch: the newest report on the default branch.
	got := decodeJSON[reportAPIDTO](t, get(f, "/api/v1/reports/bitbucket/acme/widgets"))
	if got.Commit != "new2" {
		t.Fatalf("commit = %q, want new2", got.Commit)
	}
	if got.Base == nil || got.Base.Commit != "old1" || got.Delta == nil || *got.Delta != 5 {
		t.Errorf("base = %+v, delta = %v, want old1 and +5", got.Base, got.Delta)
	}
	// One part: the upload's own page. Not a PR build: no diff.
	if got.ReportURL != "https://gocov.example/uploads/2" || got.Diff != nil {
		t.Errorf("report_url = %q, diff = %+v", got.ReportURL, got.Diff)
	}
	if got := decodeJSON[reportAPIDTO](t, get(f, "/api/v1/reports/bitbucket/acme/widgets?branch=feature")); got.Commit != "feed1234" {
		t.Errorf("?branch=feature gave %q", got.Commit)
	}
}

// What the read API may not serve looks exactly like what does not exist:
// the same 404, so it tells nobody which private repos are tracked.
func TestReportAPIServesOnlyPublicReports(t *testing.T) {
	for _, tc := range []struct {
		name       string
		visibility string
		instanceOn bool
		disabled   bool
	}{
		{"private repo", store.VisibilityPrivate, true, false},
		{"public reports off for the instance", store.VisibilityPublic, false, false},
		{"public reports off for the repo", store.VisibilityPublic, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublicFixture(t, tc.visibility, tc.instanceOn)
			f.repo.PublicReportsDisabled = tc.disabled
			if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
				t.Fatal(err)
			}
			seedCommitReport(t, f, prReport())
			hidden := get(f, "/api/v1/reports/bitbucket/acme/widgets?commit=feed1234")
			missing := get(f, "/api/v1/reports/bitbucket/acme/nothing?commit=feed1234")
			if hidden.Code != http.StatusNotFound || hidden.Body.String() != missing.Body.String() {
				t.Errorf("hidden repo: %d %q, missing repo: %d %q; want the same 404",
					hidden.Code, hidden.Body, missing.Code, missing.Body)
			}
		})
	}
}

func TestReportAPIOpenInstanceServesEveryRepo(t *testing.T) {
	f := newFixture(t, nil) // no sign-in configured
	seedCommitReport(t, f, prReport())
	rec := get(f, "/api/v1/reports/bitbucket/acme/widgets?commit=feed1234")
	if rec.Code != http.StatusOK {
		t.Fatalf("open instance: status = %d: %s", rec.Code, rec.Body)
	}
	// Nothing public about it: no shared-cache policy.
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "public") {
		t.Errorf("Cache-Control = %q on an open instance", cc)
	}
}

func TestReportAPIUnknownCommitOrEmptyBranch(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	for _, path := range []string{
		"/api/v1/reports/bitbucket/acme/widgets?commit=nope",
		"/api/v1/reports/bitbucket/acme/widgets",
	} {
		if rec := get(f, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
	}
}
