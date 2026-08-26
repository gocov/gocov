package core

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// newPipeline is a pipeline over an in-memory store with a repo already
// registered — no HTTP server, no multipart upload, no forge.
func newPipeline(t *testing.T, gate store.Gate) (*Pipeline, *storemem.Store, *store.Repo) {
	t.Helper()
	st := storemem.New()
	repo := &store.Repo{Forge: "github", Slug: "acme/widgets", Token: "tok", DefaultBranch: "main", Gate: gate}
	if err := st.CreateRepo(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	return &Pipeline{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), BaseURL: "https://cov.example.com"}, st, repo
}

// addPart stores one part's upload with a single file of covered/total
// statements, the way an upload from one CI job would land.
func addPart(t *testing.T, st *storemem.Store, repo *store.Repo, commit, part string, covered, total int64) *store.Upload {
	t.Helper()
	u := &store.Upload{
		RepoID: repo.ID, CommitSHA: commit, Branch: repo.DefaultBranch, Format: "go",
		TotalPct: profile.Percent(covered, total), CoveredStmts: covered, TotalStmts: total, Part: part,
	}
	// Coverage is recomputed from the blocks, so the file needs a covered
	// block and an uncovered one rather than a single summarised block.
	f := &store.UploadFile{
		Path: part + ".go", Pct: profile.Percent(covered, total),
		CoveredStmts: covered, TotalStmts: total,
		Blocks: []profile.Block{
			{StartLine: 1, EndLine: 2, NumStmts: int(covered), Count: 1},
			{StartLine: 3, EndLine: 4, NumStmts: int(total - covered), Count: 0},
		},
	}
	if err := st.CreateUpload(context.Background(), u, []*store.UploadFile{f}); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestRecomputeMergesEveryPart(t *testing.T) {
	p, st, repo := newPipeline(t, store.Gate{})
	ctx := context.Background()

	// One part alone: the merged report is just that part.
	back := addPart(t, st, repo, "c1", "backend", 6, 10)
	merged, err := p.Recompute(ctx, repo, back)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Upload.TotalStmts != 10 || merged.Upload.CoveredStmts != 6 {
		t.Fatalf("one part: %d/%d, want 6/10", merged.Upload.CoveredStmts, merged.Upload.TotalStmts)
	}

	// The second part joins it rather than replacing it.
	front := addPart(t, st, repo, "c1", "frontend", 9, 10)
	merged, err = p.Recompute(ctx, repo, front)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Upload.TotalStmts != 20 || merged.Upload.CoveredStmts != 15 {
		t.Fatalf("two parts: %d/%d, want 15/20", merged.Upload.CoveredStmts, merged.Upload.TotalStmts)
	}
	if merged.Upload.TotalPct != 75 {
		t.Errorf("merged total = %.1f%%, want 75%%", merged.Upload.TotalPct)
	}
	// The merged report carries the triggering upload's id, so the report
	// card and PR comment link back to the upload that produced them.
	if merged.Upload.ID != front.ID {
		t.Errorf("merged id = %d, want the triggering upload %d", merged.Upload.ID, front.ID)
	}

	// Re-uploading a part replaces it; the totals must not double-count.
	again := addPart(t, st, repo, "c1", "frontend", 2, 10)
	merged, err = p.Recompute(ctx, repo, again)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Upload.TotalStmts != 20 || merged.Upload.CoveredStmts != 8 {
		t.Fatalf("replaced part: %d/%d, want 8/20", merged.Upload.CoveredStmts, merged.Upload.TotalStmts)
	}
}

func TestRecomputeEvaluatesTheGateOnTheMergedTotal(t *testing.T) {
	// The point of merging: a part that fails on its own passes once the
	// rest of the commit is in, so a gate must never fire on one part.
	p, st, repo := newPipeline(t, store.Gate{MinCoverage: pct(70)})
	ctx := context.Background()

	back := addPart(t, st, repo, "c1", "backend", 5, 10) // 50% alone
	merged, err := p.Recompute(ctx, repo, back)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.Verdict.Failed() {
		t.Fatalf("one part at 50%% passed a 70%% gate: %v", merged.Verdict)
	}

	front := addPart(t, st, repo, "c1", "frontend", 10, 10) // 15/20 = 75% together
	merged, err = p.Recompute(ctx, repo, front)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Verdict.Failed() {
		t.Fatalf("merged 75%% failed a 70%% gate: %v", merged.Verdict.Failures)
	}
	if !merged.Verdict.Configured {
		t.Error("Configured = false for a repo with a gate")
	}
}
