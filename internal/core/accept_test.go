package core

import (
	"context"
	"testing"

	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// prof builds a one-file profile with covered of total statements.
func prof(path string, covered, total int) *profile.Profile {
	return &profile.Profile{Files: []profile.File{{
		Path: path,
		Blocks: []profile.Block{
			{StartLine: 1, EndLine: 2, NumStmts: covered, Count: 1},
			{StartLine: 3, EndLine: 4, NumStmts: total - covered, Count: 0},
		},
	}}}
}

func TestAcceptStoresMergesAndReports(t *testing.T) {
	p, st, repo := newPipeline(t, store.Gate{MinCoverage: pct(70)})
	blobs := blobmem.New()
	p.Blobs = blobs
	ctx := context.Background()

	// No forge client at all: the upload must still land, with the forge
	// surfaces reporting that they were skipped rather than failing it.
	res, err := p.Accept(ctx, Submission{
		Repo: repo, Commit: "c1", Branch: "main", Part: "default", Format: "go",
		Raw: []byte("mode: set\n"), Profile: prof("a.go", 8, 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Upload.ID == 0 {
		t.Error("upload was not stored")
	}
	if res.Upload.TotalPct != 80 || res.Merged.Upload.TotalPct != 80 {
		t.Errorf("upload %.1f%%, merged %.1f%%, want 80%%", res.Upload.TotalPct, res.Merged.Upload.TotalPct)
	}
	if res.Merged.Verdict.Failed() {
		t.Errorf("80%% failed a 70%% gate: %v", res.Merged.Verdict.Failures)
	}
	if res.Push.BuildStatus != "skipped" {
		t.Errorf("build status = %q, want %q with no forge", res.Push.BuildStatus, "skipped")
	}

	// The raw profile is kept under the key on the row, so the upload page
	// can hand back exactly what was posted.
	raw, err := blobs.Get(ctx, res.Upload.RawBlobKey)
	if err != nil {
		t.Fatalf("raw profile not stored: %v", err)
	}
	if string(raw) != "mode: set\n" {
		t.Errorf("stored profile = %q", raw)
	}

	// A second part of the same commit merges rather than replaces, and
	// the gate is re-evaluated on the total.
	res, err = p.Accept(ctx, Submission{
		Repo: repo, Commit: "c1", Branch: "main", Part: "frontend", Format: "go",
		Raw: []byte("mode: set\n"), Profile: prof("b.go", 2, 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Merged.Upload.CoveredStmts != 10 || res.Merged.Upload.TotalStmts != 20 {
		t.Fatalf("merged %d/%d, want 10/20", res.Merged.Upload.CoveredStmts, res.Merged.Upload.TotalStmts)
	}
	if !res.Merged.Verdict.Failed() {
		t.Error("merged 50% passed a 70% gate")
	}
	// The part that was just accepted keeps its own single-part numbers.
	if res.Upload.TotalPct != 20 {
		t.Errorf("this part = %.1f%%, want 20%%", res.Upload.TotalPct)
	}
	parts, err := st.LatestUploadsPerPart(ctx, repo.ID, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Errorf("commit has %d parts, want 2", len(parts))
	}
}
