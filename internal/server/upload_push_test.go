// What the forge is told after an upload, driven through the endpoint:
// build status, code insights and the PR comment, including the cases
// where there is no forge to tell. The report and annotation building is
// unit-tested in internal/core (publish_test.go); these are the
// endpoint's tests, which is why they are named for upload.go rather than
// for a file of their own.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/store"
)

var errFake = errors.New("fake forge failure")

func TestUploadWithoutForgeCredentialsSkipsStatus(t *testing.T) {
	f := newFixture(t, nil)
	rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c"}, testProfile)
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.BuildStatus != "skipped" {
		t.Errorf("build_status = %q, want skipped", resp.BuildStatus)
	}
	if len(f.forge.StatusCalls) != 0 {
		t.Errorf("forge was called despite missing credentials")
	}
}

// testPRDiff touches a.go: adds covered lines 2-3 (block 1.1,5.2 count 1),
// uncovered line 8 (block 7.1,9.2 count 0), non-executable line 20,
// plus an unmatched Go file and a doc file.

func TestUploadStatusPushSuperseded(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	ctx := t.Context()

	// First upload creates the report and pushes its status.
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1"}, testProfile)
	if len(f.forge.StatusCalls) != 1 {
		t.Fatalf("first upload: %d status calls, want 1", len(f.forge.StatusCalls))
	}

	// Simulate a newer concurrent recompute having already pushed a higher
	// version. A subsequent upload (lower version) must not overwrite the
	// forge status with its now-stale view.
	if pushed, err := f.store.TryPushStatus(ctx, f.repo.ID, "c1", 1<<30, func(context.Context) error { return nil }); err != nil || !pushed {
		t.Fatalf("setup push = %v, %v", pushed, err)
	}

	better := "mode: set\nexample.com/m/a.go:1.1,5.2 10 3\n" // 100%
	rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c1"}, better)
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.BuildStatus != "skipped: superseded" {
		t.Errorf("build_status = %q, want skipped: superseded", resp.BuildStatus)
	}
	if len(f.forge.StatusCalls) != 1 {
		t.Errorf("forge pushed a superseded status: %d calls, want still 1", len(f.forge.StatusCalls))
	}
	// The merged report itself still updated — only the forge push was held.
	if cr, err := f.store.CommitReport(ctx, f.repo.ID, "c1"); err != nil || cr.TotalPct != 100 {
		t.Errorf("report = %+v, %v (want recompute persisted 100%%)", cr, err)
	}
}

func TestPRCommentUpdatedInPlace(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})

	// First upload: no existing comment -> posted.
	rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "pr_id": "7"}, testProfile)
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.PRComment != "posted" {
		t.Fatalf("first pr_comment = %q, want posted", resp.PRComment)
	}

	// Second upload on the same PR: the existing comment is updated.
	better := "mode: set\nexample.com/m/a.go:1.1,5.2 10 3\n"
	rec = doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "pr_id": "7"}, better)
	var resp2 uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp2); err != nil {
		t.Fatal(err)
	}
	if resp2.PRComment != "updated" {
		t.Fatalf("second pr_comment = %q, want updated; body = %s", resp2.PRComment, rec.Body)
	}
	if len(f.forge.CommentCalls) != 1 {
		t.Errorf("got %d posted comments, want 1 (no stacking)", len(f.forge.CommentCalls))
	}
	if len(f.forge.UpdateCalls) != 1 {
		t.Fatalf("got %d update calls, want 1", len(f.forge.UpdateCalls))
	}
	upd := f.forge.UpdateCalls[0]
	if upd.PRID != "7" || !strings.Contains(upd.Body, "c2") || !strings.Contains(upd.Body, "100.0%") {
		t.Errorf("update call = %+v", upd)
	}
	if !strings.HasPrefix(upd.Body, core.PRCommentMarker) {
		t.Errorf("comment body must keep the marker prefix: %q", upd.Body[:40])
	}
}

func TestPRCommentUpdateFallsBackToPost(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "pr_id": "7"}, testProfile)

	t.Run("update failure posts a fresh comment", func(t *testing.T) {
		f.forge.UpdateErr = errFake
		rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "pr_id": "7"}, testProfile)
		var resp uploadResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.PRComment != "posted" {
			t.Errorf("pr_comment = %q, want posted fallback", resp.PRComment)
		}
		f.forge.UpdateErr = nil
	})

	t.Run("find failure posts a fresh comment", func(t *testing.T) {
		f.forge.FindErr = errFake
		rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c3", "pr_id": "7"}, testProfile)
		var resp uploadResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.PRComment != "posted" {
			t.Errorf("pr_comment = %q, want posted fallback", resp.PRComment)
		}
	})
}

const coveredPRDiff = `diff --git a/m/a.go b/m/a.go
--- a/m/a.go
+++ b/m/a.go
@@ -1,3 +1,5 @@
 ctx
+added 2
+added 3
 ctx
 ctx
`

func insightsUpload(t *testing.T, f *fixture, fields map[string]string) uploadResponse {
	t.Helper()
	rec := doUpload(t, f, "secret-token", fields, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestCodeInsightsPRUpload(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	f.repo.Gate = store.Gate{MinCoverage: new(float64(50))}
	if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
		t.Fatal(err)
	}
	f.forge.DiffText = testPRDiff

	fields := map[string]string{"commit": "prcommit1", "branch": "feature/x", "pr_id": "42"}
	resp := insightsUpload(t, f, fields)
	if resp.CodeInsights != "posted" {
		t.Fatalf("code_insights = %q, want posted", resp.CodeInsights)
	}
	if len(f.forge.ReportCalls) != 1 {
		t.Fatalf("report calls = %d, want 1", len(f.forge.ReportCalls))
	}
	call := f.forge.ReportCalls[0]
	if call.RepoSlug != "acme/widgets" || call.CommitSHA != "prcommit1" {
		t.Errorf("report call target = %s@%s", call.RepoSlug, call.CommitSHA)
	}
	r := call.Report
	if r.Title != "gocov coverage" || r.Result != forge.ReportPassed {
		t.Errorf("report = %q result %q", r.Title, r.Result)
	}
	if !strings.Contains(r.Link, "/uploads/") {
		t.Errorf("report link = %q, want upload page link", r.Link)
	}
	if !strings.Contains(r.Details, "2 of 3 changed lines are covered") {
		t.Errorf("report details = %q", r.Details)
	}

	// Data fields in PRD order: total, diff pct, uncovered count,
	// statements, gate (no delta on a first upload), then the per-file
	// summary filling the remaining budget.
	titles := make([]string, len(r.Data))
	for i, d := range r.Data {
		titles[i] = d.Title
	}
	want := []string{"Total coverage", "Diff coverage", "Uncovered changed lines", "Statements", "Gate", "m/a.go"}
	if strings.Join(titles, ",") != strings.Join(want, ",") {
		t.Fatalf("data fields = %v, want %v", titles, want)
	}
	if r.Data[5].Type != forge.DataPercentage {
		t.Errorf("per-file field = %+v", r.Data[5])
	}
	if r.Data[0].Type != forge.DataPercentage || r.Data[0].Value != 80.0 {
		t.Errorf("total coverage field = %+v", r.Data[0])
	}
	if got := r.Data[1].Value.(float64); math.Abs(got-200.0/3.0) > 1e-9 {
		t.Errorf("diff coverage = %v, want 66.67", got)
	}
	if r.Data[2].Type != forge.DataNumber || r.Data[2].Value != 1.0 {
		t.Errorf("uncovered changed lines field = %+v", r.Data[2])
	}
	if r.Data[3].Value != "8 / 10" || r.Data[4].Value != "passed" {
		t.Errorf("statements/gate = %v / %v", r.Data[3].Value, r.Data[4].Value)
	}

	// Two annotations: the whole-file finding for untested.go (file
	// level, no line) comes first, then a.go line 9 — the only
	// uncovered changed line with coverage data.
	if len(call.Annotations) != 2 {
		t.Fatalf("annotations = %+v, want exactly 2", call.Annotations)
	}
	if a := call.Annotations[0]; a.Path != "m/untested.go" || a.Line != 0 ||
		!strings.Contains(a.Summary, "no coverage data") {
		t.Errorf("file-level annotation = %+v", a)
	}
	if a := call.Annotations[1]; a.Path != "m/a.go" || a.Line != 9 ||
		a.Summary != "Line 9 of this change is not covered by tests" {
		t.Errorf("line annotation = %+v", a)
	}

	// Re-upload for the same commit recomputes its merged report and
	// publishes again under the same commit key (the forge replaces in
	// place). It carries no "Change vs base": the merged delta compares
	// against the previous *distinct* commit, and a commit is never its own
	// baseline, so re-running CI on one commit shows no phantom delta.
	resp = insightsUpload(t, f, fields)
	if resp.CodeInsights != "posted" || len(f.forge.ReportCalls) != 2 {
		t.Fatalf("re-upload: code_insights = %q, %d report calls", resp.CodeInsights, len(f.forge.ReportCalls))
	}
	second := f.forge.ReportCalls[1]
	if second.CommitSHA != "prcommit1" {
		t.Errorf("re-upload commit = %q", second.CommitSHA)
	}
	for _, d := range second.Report.Data {
		if d.Title == "Change vs base" {
			t.Errorf("re-upload of the same commit should carry no delta, got %+v", d)
		}
	}
}

func TestCodeInsightsFullyCoveredDiff(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	f.repo.Gate = store.Gate{MinDiffCoverage: new(float64(100))}
	if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
		t.Fatal(err)
	}
	f.forge.DiffText = coveredPRDiff

	resp := insightsUpload(t, f, map[string]string{"commit": "c1", "branch": "feature/x", "pr_id": "7"})
	if resp.CodeInsights != "posted" {
		t.Fatalf("code_insights = %q", resp.CodeInsights)
	}
	call := f.forge.ReportCalls[0]
	if call.Report.Result != forge.ReportPassed {
		t.Errorf("result = %q, want passed at 100%% diff coverage", call.Report.Result)
	}
	if len(call.Annotations) != 0 {
		t.Errorf("annotations = %+v, want none", call.Annotations)
	}
	for _, d := range call.Report.Data {
		if d.Title == "Uncovered changed lines" && d.Value != 0.0 {
			t.Errorf("uncovered changed lines = %v, want 0", d.Value)
		}
	}
}

func TestCodeInsightsNonPRUploadHasNoAnnotations(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})

	resp := insightsUpload(t, f, map[string]string{"commit": "c1", "branch": "main"})
	if resp.CodeInsights != "posted" {
		t.Fatalf("code_insights = %q", resp.CodeInsights)
	}
	call := f.forge.ReportCalls[0]
	if len(call.Annotations) != 0 {
		t.Errorf("annotations on a non-PR upload: %+v", call.Annotations)
	}
	// No gate configured: data without a verdict.
	if call.Report.Result != "" {
		t.Errorf("result = %q, want no verdict without a gate", call.Report.Result)
	}
	for _, d := range call.Report.Data {
		if d.Title == "Diff coverage" || d.Title == "Gate" {
			t.Errorf("unexpected data field %q on a non-PR, no-gate upload", d.Title)
		}
	}
}

func TestCodeInsightsSkippedWithoutCredentials(t *testing.T) {
	f := newFixture(t, nil)

	resp := insightsUpload(t, f, map[string]string{"commit": "c1"})
	if resp.CodeInsights != "skipped" {
		t.Errorf("code_insights = %q, want skipped", resp.CodeInsights)
	}
	if len(f.forge.ReportCalls) != 0 {
		t.Errorf("report calls without credentials: %+v", f.forge.ReportCalls)
	}
}

// TestCodeInsightsSkippedWithReason covers a forge that wraps
// ErrNotImplemented with an explanation — e.g. GitHub check runs being
// closed to the credential type — which the response surfaces instead
// of a bare "skipped".

func TestCodeInsightsSkippedWithReason(t *testing.T) {
	f := newFixture(t, map[string]string{"token": "t"})
	f.forge.ReportErr = fmt.Errorf("github: %w: this GitHub credential cannot write check runs", forge.ErrNotImplemented)

	resp := insightsUpload(t, f, map[string]string{"commit": "c1"})
	want := "skipped: github: " + forge.ErrNotImplemented.Error() + ": this GitHub credential cannot write check runs"
	if resp.CodeInsights != want {
		t.Errorf("code_insights = %q, want %q", resp.CodeInsights, want)
	}
}

// TestCodeInsightsFailureIsolation stubs the insights push to fail and
// verifies the rest of the upload — response, build status, PR comment —
// is identical to a healthy run.

func TestCodeInsightsFailureIsolation(t *testing.T) {
	fields := map[string]string{"commit": "prcommit1", "branch": "feature/x", "pr_id": "42"}

	healthy := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	healthy.forge.DiffText = testPRDiff
	wantResp := insightsUpload(t, healthy, fields)

	broken := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	broken.forge.DiffText = testPRDiff
	broken.forge.ReportErr = errors.New("bitbucket: reports returned 502: bad gateway")
	resp := insightsUpload(t, broken, fields)

	if resp.CodeInsights != "error: bitbucket: reports returned 502: bad gateway" {
		t.Errorf("code_insights = %q", resp.CodeInsights)
	}
	resp.CodeInsights = wantResp.CodeInsights
	got, _ := json.Marshal(resp)
	want, _ := json.Marshal(wantResp)
	if string(got) != string(want) {
		t.Errorf("upload response diverged beyond code_insights:\ngot  %s\nwant %s", got, want)
	}
	if len(broken.forge.StatusCalls) != 1 ||
		broken.forge.StatusCalls[0] != healthy.forge.StatusCalls[0] {
		t.Errorf("build status diverged: %+v", broken.forge.StatusCalls)
	}
	if len(broken.forge.CommentCalls) != 1 ||
		broken.forge.CommentCalls[0].Body != healthy.forge.CommentCalls[0].Body {
		t.Errorf("PR comment diverged")
	}
}
