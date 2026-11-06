package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/forge/fake"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

func TestInsightsAnnotationsCollapseRanges(t *testing.T) {
	dc := &diffcov.Result{
		Files: []diffcov.FileCoverage{
			{Path: "a.go", UncoveredLines: []int{5, 6, 7, 10}},
			{Path: "b.go", UncoveredLines: []int{1}},
		},
		UnmatchedFiles: []string{"c.go"},
	}
	anns, dropped := insightsAnnotations(dc)
	if dropped != 0 {
		t.Fatalf("dropped = %d", dropped)
	}
	want := []forge.Annotation{
		{Path: "c.go", Summary: "This changed file has no coverage data — nothing in it appears to be tested"},
		{Path: "a.go", Line: 5, EndLine: 7, Summary: "Lines 5–7 of this change are not covered by tests"},
		{Path: "a.go", Line: 10, EndLine: 10, Summary: "Line 10 of this change is not covered by tests"},
		{Path: "b.go", Line: 1, EndLine: 1, Summary: "Line 1 of this change is not covered by tests"},
	}
	if len(anns) != len(want) {
		t.Fatalf("annotations = %+v", anns)
	}
	for i := range want {
		if anns[i] != want[i] {
			t.Errorf("annotation[%d] = %+v, want %+v", i, anns[i], want[i])
		}
	}
}

func TestInsightsAnnotationsTruncate(t *testing.T) {
	p := &Pipeline{BaseURL: "https://cov.example.com"}
	// 105 non-contiguous uncovered lines: odd numbers 1..209.
	lines := make([]int, 105)
	for i := range lines {
		lines[i] = 2*i + 1
	}
	dc := &diffcov.Result{
		Files:        []diffcov.FileCoverage{{Path: "big.go", UncoveredLines: lines}},
		TotalLines:   105,
		CoveredLines: 0,
	}
	anns, dropped := insightsAnnotations(dc)
	if len(anns) != insightsMaxAnnotations || dropped != 5 {
		t.Fatalf("got %d annotations, %d dropped; want 100/5", len(anns), dropped)
	}

	// The truncation is called out in the report details.
	report, _ := p.insightsReport(&store.Upload{
		TotalPct: 80, CoveredStmts: 8, TotalStmts: 10, DiffCoverage: dc,
	}, "", nil, Verdict{})
	if !strings.Contains(report.Details, "+5 more uncovered ranges") {
		t.Errorf("details = %q, want truncation note", report.Details)
	}
}

func TestPRCommentSignatureHostedOnly(t *testing.T) {
	u := &store.Upload{CommitSHA: "abc123", TotalPct: 80}

	selfHosted := &Pipeline{BaseURL: "https://cov.example.com"}
	if body := selfHosted.prCommentBody(u, "", nil, Verdict{}); strings.Contains(body, "gocov.dev") {
		t.Errorf("self-hosted PR comment carries the signature line:\n%s", body)
	}

	hosted := &Pipeline{BaseURL: "https://cov.example.com", Hosted: true}
	body := hosted.prCommentBody(u, "", nil, Verdict{})
	if !strings.Contains(body, "<sub>Coverage by [gocov](https://gocov.dev?ref=pr-comment) — free for public repos</sub>") {
		t.Errorf("hosted PR comment misses the signature line:\n%s", body)
	}
	// Update-in-place matches on the leading marker; the signature must
	// not disturb it.
	if !strings.HasPrefix(body, PRCommentMarker) {
		t.Errorf("comment no longer starts with the update-in-place marker:\n%s", body)
	}
}

func TestInsightsPerFileDataBudget(t *testing.T) {
	p := &Pipeline{BaseURL: "https://cov.example.com"}
	// Eight partially covered files against six standard fields (delta
	// and gate present): the per-file summary must stop at the API's
	// ten-field cap, worst-covered file first.
	files := make([]diffcov.FileCoverage, 8)
	for i := range files {
		files[i] = diffcov.FileCoverage{
			Path:           string(rune('a'+i)) + ".go",
			CoveredLines:   int64(i), // i of 10 covered: a.go worst
			TotalLines:     10,
			UncoveredLines: []int{1}, // marker: has uncovered lines
		}
	}
	dc := &diffcov.Result{Files: files, CoveredLines: 28, TotalLines: 80}

	delta := 1.5
	report, _ := p.insightsReport(&store.Upload{
		TotalPct: 80, CoveredStmts: 8, TotalStmts: 10, DiffCoverage: dc,
	}, "", &delta, Verdict{Configured: true})

	if len(report.Data) != insightsMaxDataFields {
		t.Fatalf("data fields = %d, want %d", len(report.Data), insightsMaxDataFields)
	}
	// Six standard fields, then the four worst files a.go .. d.go.
	for i, wantTitle := range []string{"a.go", "b.go", "c.go", "d.go"} {
		d := report.Data[6+i]
		if d.Title != wantTitle || d.Type != forge.DataPercentage || d.Value != float64(i*10) {
			t.Errorf("per-file field[%d] = %+v, want %s at %d%%", i, d, wantTitle, i*10)
		}
	}
}

func TestInsightsFullyCoveredFilesClaimNoDataFields(t *testing.T) {
	p := &Pipeline{BaseURL: "https://cov.example.com"}
	dc := &diffcov.Result{
		Files: []diffcov.FileCoverage{
			{Path: "ok.go", CoveredLines: 5, TotalLines: 5},
			{Path: "bad.go", CoveredLines: 0, TotalLines: 2, UncoveredLines: []int{3, 4}},
		},
		CoveredLines: 5, TotalLines: 7,
	}
	report, _ := p.insightsReport(&store.Upload{
		TotalPct: 80, CoveredStmts: 8, TotalStmts: 10, DiffCoverage: dc,
	}, "", nil, Verdict{})
	for _, d := range report.Data {
		if d.Title == "ok.go" {
			t.Errorf("fully covered file claimed a data field: %+v", d)
		}
	}
	last := report.Data[len(report.Data)-1]
	if last.Title != "bad.go" || last.Value != 0.0 {
		t.Errorf("per-file field = %+v, want bad.go at 0%%", last)
	}
}

// seedRepoUpload registers a repo and one upload with a single file, so the
// repo, upload and source pages all have something to render.

// barrierForge holds its build status and report publish until both are in
// flight, so a push that ran the surfaces one after another would stall.
type barrierForge struct {
	*fake.Forge
	arrived sync.WaitGroup
}

func (b *barrierForge) wait(ctx context.Context) error {
	b.arrived.Done()
	done := make(chan struct{})
	go func() { b.arrived.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *barrierForge) PostBuildStatus(ctx context.Context, repoSlug, commitSHA string, status forge.BuildStatus) error {
	if err := b.wait(ctx); err != nil {
		return err
	}
	return b.Forge.PostBuildStatus(ctx, repoSlug, commitSHA, status)
}

func (b *barrierForge) PublishReport(ctx context.Context, repoSlug, commitSHA string, report forge.Report, annotations []forge.Annotation) error {
	if err := b.wait(ctx); err != nil {
		return err
	}
	return b.Forge.PublishReport(ctx, repoSlug, commitSHA, report, annotations)
}

func TestPushSurfacesRunConcurrently(t *testing.T) {
	fg := &barrierForge{Forge: &fake.Forge{}}
	fg.arrived.Add(2)
	p := &Pipeline{BaseURL: "https://cov.example.com", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	res := p.pushSurfaces(ctx, fg, &store.Repo{Slug: "acme/api"},
		&store.Upload{CommitSHA: "abc", PRID: "7", TotalPct: 80}, "", nil, Verdict{})
	if res.BuildStatus != "posted" || res.CodeInsights != "posted" || res.PRComment != "posted" {
		t.Fatalf("push result = %+v, want every surface posted", res)
	}
	if pt := res.timings; !pt.ran || pt.status == 0 || pt.insights == 0 || pt.comment == 0 {
		t.Errorf("push timings = %+v, want every surface timed", pt)
	}
}

func TestPRCommentBody(t *testing.T) {
	p := &Pipeline{BaseURL: "https://cov.example.com"}
	delta := -1.24
	u := &store.Upload{
		CommitSHA: "0123456789abcdef0123", TotalPct: 81.24,
		DiffCoverage: &diffcov.Result{
			TotalLines: 10, CoveredLines: 7,
			Files: []diffcov.FileCoverage{
				{Path: "ok.go", CoveredLines: 4, TotalLines: 4},
				{Path: "pkg/a|b`c.go", CoveredLines: 3, TotalLines: 6, UncoveredLines: []int{3, 4, 5, 9}},
			},
			UnmatchedFiles: []string{"gen.go", "new\nfile.go"},
		},
	}
	gate := Verdict{Configured: true, Failures: []string{"total coverage 81.24% is below the minimum 90%", "diff coverage 70% is below the minimum 80%"}}

	got := p.prCommentBody(u, "https://cov.example.com/uploads/7", &delta, gate)
	want := "**gocov** report for `0123456789ab`\n\n" +
		"- Total coverage: **81.2%** (-1.2%)\n" +
		"- Gate: ❌ total coverage 81.24% is below the minimum 90%; diff coverage 70% is below the minimum 80%\n" +
		"- Diff coverage: **70.0%** (7/10 changed lines covered)\n" +
		"\nUncovered changed lines:\n\n| File | Lines |\n| --- | --- |\n" +
		"| `pkg/a\\|b'c.go` | 3-5, 9 |\n" +
		"\nChanged files without coverage data: `gen.go`, `new file.go`\n" +
		"\n[Full report](https://cov.example.com/uploads/7)\n"
	if got != want {
		t.Errorf("PR comment body =\n%s\nwant\n%s", got, want)
	}
}

func TestPRCommentBodyGateAndDiffStates(t *testing.T) {
	p := &Pipeline{BaseURL: "https://cov.example.com"}
	for _, tc := range []struct {
		name    string
		diff    *diffcov.Result
		gate    Verdict
		want    []string
		mustNot []string
	}{
		{
			name:    "no gate and no diff",
			want:    []string{"- Total coverage: **80.0%**\n"},
			mustNot: []string{"Gate:", "Diff coverage:", "Uncovered changed lines"},
		},
		{
			name: "passing gate",
			gate: Verdict{Configured: true},
			want: []string{"- Gate: ✅ passed\n"},
		},
		{
			name:    "diff touching no executable lines",
			diff:    &diffcov.Result{},
			want:    []string{"- Diff coverage: no executable lines changed\n"},
			mustNot: []string{"Uncovered changed lines", "without coverage data"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := p.prCommentBody(&store.Upload{CommitSHA: "abc", TotalPct: 80, DiffCoverage: tc.diff}, "", nil, tc.gate)
			for _, w := range tc.want {
				if !strings.Contains(body, w) {
					t.Errorf("body misses %q:\n%s", w, body)
				}
			}
			for _, w := range tc.mustNot {
				if strings.Contains(body, w) {
					t.Errorf("body carries %q:\n%s", w, body)
				}
			}
		})
	}
}

func TestPRCommentBodyCapsFileLists(t *testing.T) {
	p := &Pipeline{BaseURL: "https://cov.example.com"}
	const n = prCommentMaxFiles + 3
	files := make([]diffcov.FileCoverage, n)
	unmatched := make([]string, n)
	for i := range n {
		files[i] = diffcov.FileCoverage{Path: fmt.Sprintf("f%02d.go", i), TotalLines: 1, UncoveredLines: []int{1}}
		unmatched[i] = fmt.Sprintf("u%02d.go", i)
	}
	body := p.prCommentBody(&store.Upload{
		CommitSHA: "abc", TotalPct: 0,
		DiffCoverage: &diffcov.Result{TotalLines: n, Files: files, UnmatchedFiles: unmatched},
	}, "", nil, Verdict{})

	if got := strings.Count(body, "| `f"); got != prCommentMaxFiles {
		t.Errorf("table rows = %d, want %d", got, prCommentMaxFiles)
	}
	if !strings.Contains(body, "| … | and 3 more files |\n") {
		t.Errorf("table misses the overflow row:\n%s", body)
	}
	if strings.Contains(body, fmt.Sprintf("f%02d.go", prCommentMaxFiles)) {
		t.Errorf("table lists a file past the cap:\n%s", body)
	}
	if !strings.Contains(body, fmt.Sprintf("`u%02d.go` and 3 more\n", prCommentMaxFiles-1)) {
		t.Errorf("unmatched list misses its overflow tail:\n%s", body)
	}
	if strings.Contains(body, fmt.Sprintf("u%02d.go", prCommentMaxFiles)) {
		t.Errorf("unmatched list names a file past the cap:\n%s", body)
	}
}

func TestPushPRComment(t *testing.T) {
	ctx := t.Context()
	p := &Pipeline{BaseURL: "https://cov.example.com", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	repo := &store.Repo{Slug: "acme/api"}

	t.Run("non-PR upload leaves the field empty", func(t *testing.T) {
		fg := fake.New()
		if got := p.pushPRComment(ctx, fg, repo, &store.Upload{CommitSHA: "abc"}, "", nil, Verdict{}); got != "" {
			t.Errorf("result = %q, want empty", got)
		}
		if len(fg.CommentCalls)+len(fg.FindCalls) != 0 {
			t.Errorf("non-PR upload reached the forge: %+v", fg)
		}
	})

	t.Run("second upload updates the comment in place", func(t *testing.T) {
		fg := fake.New()
		u := &store.Upload{CommitSHA: "abc", PRID: "7", TotalPct: 80}
		if got := p.pushPRComment(ctx, fg, repo, u, "", nil, Verdict{}); got != "posted" {
			t.Fatalf("first push = %q, want posted", got)
		}
		u.TotalPct = 90
		if got := p.pushPRComment(ctx, fg, repo, u, "", nil, Verdict{}); got != "updated" {
			t.Fatalf("second push = %q, want updated", got)
		}
		if len(fg.CommentCalls) != 1 || len(fg.UpdateCalls) != 1 {
			t.Fatalf("posts = %d, updates = %d; want 1 and 1", len(fg.CommentCalls), len(fg.UpdateCalls))
		}
		if !strings.Contains(fg.UpdateCalls[0].Body, "**90.0%**") {
			t.Errorf("update body = %q, want the new total", fg.UpdateCalls[0].Body)
		}
		if fg.FindCalls[0] != PRCommentMarker {
			t.Errorf("searched for %q, want the marker %q", fg.FindCalls[0], PRCommentMarker)
		}
	})

	t.Run("failed update falls back to a fresh comment", func(t *testing.T) {
		fg := fake.New()
		u := &store.Upload{CommitSHA: "abc", PRID: "7"}
		p.pushPRComment(ctx, fg, repo, u, "", nil, Verdict{})
		fg.UpdateErr = errors.New("forge down")
		if got := p.pushPRComment(ctx, fg, repo, u, "", nil, Verdict{}); got != "posted" {
			t.Errorf("result = %q, want posted", got)
		}
		if len(fg.CommentCalls) != 2 {
			t.Errorf("posts = %d, want the fallback second post", len(fg.CommentCalls))
		}
	})

	t.Run("failed lookup still posts", func(t *testing.T) {
		fg := fake.New()
		fg.FindErr = errors.New("forge down")
		if got := p.pushPRComment(ctx, fg, repo, &store.Upload{CommitSHA: "abc", PRID: "7"}, "", nil, Verdict{}); got != "posted" {
			t.Errorf("result = %q, want posted", got)
		}
	})

	t.Run("failed post is reported", func(t *testing.T) {
		fg := fake.New()
		fg.CommentErr = errors.New("forbidden")
		if got := p.pushPRComment(ctx, fg, repo, &store.Upload{CommitSHA: "abc", PRID: "7"}, "", nil, Verdict{}); got != "error: forbidden" {
			t.Errorf("result = %q, want error: forbidden", got)
		}
	})
}

func TestPushBuildStatus(t *testing.T) {
	ctx := t.Context()
	p := &Pipeline{BaseURL: "https://cov.example.com", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	repo := &store.Repo{Slug: "acme/api"}
	u := &store.Upload{CommitSHA: "abc", TotalPct: 72.34}
	delta := 0.5

	fg := fake.New()
	if got := p.pushBuildStatus(ctx, fg, repo, u, "https://link", &delta, Verdict{Configured: true}); got != "posted" {
		t.Fatalf("result = %q, want posted", got)
	}
	st := fg.StatusCalls[0].Status
	if st.State != forge.StateSuccessful || st.Description != "coverage: 72.3% (+0.5%)" || st.URL != "https://link" {
		t.Errorf("passing status = %+v", st)
	}

	// A failed gate fails the status and names the first reason only.
	failed := Verdict{Configured: true, Failures: []string{"first reason", "second reason"}}
	p.pushBuildStatus(ctx, fg, repo, u, "", nil, failed)
	st = fg.StatusCalls[1].Status
	if st.State != forge.StateFailed || st.Description != "coverage: 72.3% — first reason" {
		t.Errorf("failing status = %+v", st)
	}

	fg.StatusErr = errors.New("rate limited")
	if got := p.pushBuildStatus(ctx, fg, repo, u, "", nil, Verdict{}); got != "error: rate limited" {
		t.Errorf("result = %q, want error: rate limited", got)
	}
}

func TestPushCodeInsightsOutcomes(t *testing.T) {
	ctx := t.Context()
	p := &Pipeline{BaseURL: "https://cov.example.com", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	repo := &store.Repo{Slug: "acme/api"}
	u := &store.Upload{CommitSHA: "abc"}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "posted", want: "posted"},
		{name: "forge without the surface", err: forge.ErrNotImplemented, want: "skipped"},
		{
			name: "surface closed with a reason",
			err:  fmt.Errorf("check runs need a GitHub App: %w", forge.ErrNotImplemented),
			want: "skipped: check runs need a GitHub App: forge: not implemented",
		},
		{name: "forge error", err: errors.New("boom"), want: "error: boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fg := fake.New()
			fg.ReportErr = tc.err
			if got := p.pushCodeInsights(ctx, fg, repo, u, "", nil, Verdict{}); got != tc.want {
				t.Errorf("result = %q, want %q", got, tc.want)
			}
		})
	}
}

// newConnectedPipeline is newPipeline with the repo's workspace connected
// through fg, as the background follow-up resolves its client afresh.
func newConnectedPipeline(t *testing.T, fg forge.Forge) (*Pipeline, *storemem.Store, *store.Repo) {
	t.Helper()
	forges, st := newForges(t, &fakeBB{client: fg})
	connectedWorkspace(t, st, "acme")
	repo := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "tok", DefaultBranch: "main"}
	if err := st.CreateRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	return &Pipeline{Store: st, Log: forges.Log, Forges: forges, BaseURL: "https://cov.example.com"}, st, repo
}

// An upload landing while another upload of its commit pushes does not
// wait: it reports queued. The push in flight answers its own upload, then
// goes round again in the background with the commit's newest state, so
// the forge ends on both parts merged.
func TestPushQueuesBehindAPushInFlight(t *testing.T) {
	fg := fake.New()
	entered, release := fg.HoldFirstStatus()
	p, st, repo := newConnectedPipeline(t, fg)
	ctx := t.Context()

	back := addPart(t, st, repo, "c1", "backend", 8, 10)
	backMerged, err := p.Recompute(ctx, repo, back)
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan PushResult)
	go func() { first <- p.Push(ctx, fg, repo, back, backMerged) }()
	<-entered

	front := addPart(t, st, repo, "c1", "frontend", 2, 10)
	frontMerged, err := p.Recompute(ctx, repo, front)
	if err != nil {
		t.Fatal(err)
	}
	queued := p.Push(ctx, fg, repo, front, frontMerged)
	if queued.BuildStatus != Queued || queued.CodeInsights != Queued || queued.timings.ran {
		t.Errorf("second push = %+v, want queued on every surface", queued)
	}

	close(release)
	if res := <-first; res.BuildStatus != "posted" {
		t.Errorf("first push = %+v, want posted", res)
	}
	if err := p.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(fg.StatusCalls); n != 2 {
		t.Fatalf("%d status calls, want 2", n)
	}
	if got := fg.StatusCalls[1].Status.Description; got != "coverage: 50.0%" {
		t.Errorf("last status = %q, want both parts merged (coverage: 50.0%%)", got)
	}
	// The lease is free again and the forge has seen the newest upload:
	// the commit's next push is a plain one.
	claim, lease, err := st.ClaimPush(ctx, repo.ID, "c1", front.ID, time.Minute)
	if err != nil || claim != store.PushClaimed {
		t.Fatalf("claim after the follow-up = %v, %v; want the lease released", claim, err)
	}
	if again, err := st.FinishPush(ctx, repo.ID, "c1", front.ID, lease, true); err != nil || again {
		t.Errorf("finish after the follow-up = %v, %v; want nothing left", again, err)
	}
}

// commitBehind pushes the commit at version, and reports whether the
// store then says the forge is behind the commit's newest upload.
func commitBehind(t *testing.T, st store.Store, repo *store.Repo, version int64) bool {
	t.Helper()
	claim, lease, err := st.ClaimPush(t.Context(), repo.ID, "c1", version, time.Minute)
	if err != nil || claim != store.PushClaimed {
		t.Fatalf("claim v%d = %v, %v", version, claim, err)
	}
	again, err := st.FinishPush(t.Context(), repo.ID, "c1", version, lease, true)
	if err != nil {
		t.Fatal(err)
	}
	return again
}

// A follow-up that cannot reload the commit stops, and loses nothing:
// the upload the forge has not seen is still newer than what was pushed,
// so the commit's next push is told to go round.
func TestPushFollowUpFailureLosesNothing(t *testing.T) {
	fg := fake.New()
	entered, release := fg.HoldFirstStatus()
	p, st, repo := newConnectedPipeline(t, fg)
	ctx := t.Context()

	back := addPart(t, st, repo, "c1", "backend", 8, 10)
	backMerged, err := p.Recompute(ctx, repo, back)
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan PushResult)
	go func() { first <- p.Push(ctx, fg, repo, back, backMerged) }()
	<-entered
	front := addPart(t, st, repo, "c1", "frontend", 2, 10)
	frontMerged, err := p.Recompute(ctx, repo, front)
	if err != nil {
		t.Fatal(err)
	}
	if q := p.Push(ctx, fg, repo, front, frontMerged); q.BuildStatus != Queued {
		t.Fatalf("second push = %q, want queued", q.BuildStatus)
	}

	p.Store = reloadErrStore{st} // the follow-up's reload fails
	close(release)
	<-first
	if err := p.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(fg.StatusCalls); n != 1 {
		t.Fatalf("%d status calls, want only the holder's own", n)
	}
	if !commitBehind(t, st, repo, back.ID) {
		t.Error("the store says the forge is up to date; the frontend upload was lost")
	}
}

type reloadErrStore struct{ store.Store }

func (reloadErrStore) LatestUploadsPerPart(context.Context, int64, string) ([]*store.Upload, error) {
	return nil, errors.New("pool closed")
}

// A push that cannot claim says so on every surface rather than leaving
// them blank.
func TestPushReportsAClaimFailure(t *testing.T) {
	p, st, repo := newPipeline(t, store.Gate{})
	u := addBranchPart(t, st, repo, "c1", "feature", "7", "default", 8, 10)
	merged, err := p.Recompute(t.Context(), repo, u)
	if err != nil {
		t.Fatal(err)
	}
	p.Store = claimErrStore{st}
	res := p.Push(t.Context(), fake.New(), repo, u, merged)
	if !strings.HasPrefix(res.BuildStatus, "error: ") || res.PRComment != res.BuildStatus || res.timings.ran {
		t.Errorf("push = %+v, want an error on every surface and nothing pushed", res)
	}
}

type claimErrStore struct{ store.Store }

func (claimErrStore) ClaimPush(context.Context, int64, string, int64, time.Duration) (store.PushClaim, time.Time, error) {
	return 0, time.Time{}, errors.New("pool closed")
}

// A shutdown that runs out of patience cuts a follow-up short instead of
// waiting out a forge that stopped answering, early enough for it to
// release its lease, and loses nothing: the upload it was pushing is still
// newer than what the forge has.
func TestDrainCutsAStuckFollowUpShort(t *testing.T) {
	fg := fake.New()
	stuck := make(chan struct{})
	fg.StatusHook = func(ctx context.Context, n int) error {
		if n == 1 {
			return nil
		}
		close(stuck)
		<-ctx.Done()
		return ctx.Err()
	}
	p, st, repo := newConnectedPipeline(t, fg)
	ctx := t.Context()

	// The frontend part is in before the backend's push finishes, so the
	// push goes round again for it — and that round hangs.
	back := addPart(t, st, repo, "c1", "backend", 8, 10)
	backMerged, err := p.Recompute(ctx, repo, back)
	if err != nil {
		t.Fatal(err)
	}
	front := addPart(t, st, repo, "c1", "frontend", 2, 10)
	if _, err := p.Recompute(ctx, repo, front); err != nil {
		t.Fatal(err)
	}
	if res := p.Push(ctx, fg, repo, back, backMerged); res.BuildStatus != "posted" {
		t.Fatalf("holder push = %q, want posted", res.BuildStatus)
	}
	<-stuck

	unwind := finishPushTimeout + time.Second
	dctx, cancel := context.WithTimeout(ctx, unwind+20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := p.Drain(dctx); !errors.Is(err, errBackgroundCut) {
		t.Fatalf("drain = %v, want the follow-up cut short", err)
	}
	if d := time.Since(start); d > unwind {
		t.Errorf("drain took %v, want it to cut the hung round short, not wait out its unwind time", d)
	}
	if !commitBehind(t, st, repo, back.ID) {
		t.Error("the store says the forge is up to date; the frontend upload was lost")
	}
}
