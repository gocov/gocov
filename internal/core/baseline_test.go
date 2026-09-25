package core

import (
	"fmt"
	"testing"

	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

func TestReportBaseline(t *testing.T) {
	report := func(id int64, pct float64, failed bool) *store.CommitReport {
		return &store.CommitReport{ID: id, UploadID: id, CommitSHA: fmt.Sprintf("c%d", id), TotalPct: pct, GateFailed: failed}
	}
	ok := func(id int64) *store.CommitReport { return report(id, 80, false) }
	failed := func(id int64) *store.CommitReport { return report(id, 50, true) }
	prBuild := func(id int64) *store.CommitReport {
		cr := ok(id)
		cr.PRID = "7"
		return cr
	}
	for _, tc := range []struct {
		name          string
		reports       []*store.CommitReport // newest first
		wantCur, want int64                 // report ids, 0 for nil
	}{
		{"no reports", nil, 0, 0},
		{"single report", []*store.CommitReport{ok(1)}, 1, 0},
		{"previous passed", []*store.CommitReport{ok(2), ok(1)}, 2, 1},
		// A failed report never becomes the baseline; the delta reads
		// against the last one that passed.
		{"skips failed", []*store.CommitReport{ok(3), failed(2), ok(1)}, 3, 1},
		{"all earlier failed", []*store.CommitReport{ok(3), failed(2), failed(1)}, 3, 0},
		// The newest report is the current one whether or not it passed.
		{"current failed", []*store.CommitReport{failed(2), ok(1)}, 2, 1},
		// A feature branch's trend carries its PR's builds; the newest one
		// is current, but none is a baseline.
		{"skips PR builds", []*store.CommitReport{prBuild(3), prBuild(2), ok(1)}, 3, 1},
		{"only PR builds", []*store.CommitReport{prBuild(2), prBuild(1)}, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := func(r *store.CommitReport) int64 {
				if r == nil {
					return 0
				}
				return r.ID
			}
			cur, base := ReportBaseline(tc.reports)
			if id(cur) != tc.wantCur || id(base) != tc.want {
				t.Errorf("ReportBaseline = %d, %d, want %d, %d", id(cur), id(base), tc.wantCur, tc.want)
			}
		})
	}
}

// UploadBaseline finds the branch's newest earlier passing build however
// many PR builds sit on top of it, and falls back to the default branch —
// never the upload's own commit there — for a branch with none.
func TestUploadBaseline(t *testing.T) {
	ctx := t.Context()
	st := storemem.New()
	repo := &store.Repo{Forge: "github", Slug: "acme/widgets", Token: "tok", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	put := func(sha, branch, prID string) *store.Upload {
		t.Helper()
		u := &store.Upload{RepoID: repo.ID, CommitSHA: sha, Branch: branch, PRID: prID, Format: "go"}
		if err := st.CreateUpload(ctx, u, nil); err != nil {
			t.Fatal(err)
		}
		return u
	}
	sha := func(u *store.Upload) string {
		if u == nil {
			return "none"
		}
		return u.CommitSHA
	}

	first := put("m1", "main", "")
	if got := UploadBaseline(ctx, st, repo, first); got != nil {
		t.Errorf("first upload's baseline = %s, want none", sha(got))
	}
	put("f0", "feat", "")
	// More PR builds than any fixed window a scan would read.
	for i := range 70 {
		put(fmt.Sprintf("p%d", i), "feat", "7")
	}
	pr := put("p70", "feat", "7")
	if got := UploadBaseline(ctx, st, repo, pr); sha(got) != "f0" {
		t.Errorf("PR build's baseline = %s, want the branch's push build f0 under 70 PR builds", sha(got))
	}
	// A branch with no passing build of its own falls back to main, but
	// not to main's upload of this very commit.
	put("x1", "main", "")
	fresh := put("x1", "fresh", "")
	if got := UploadBaseline(ctx, st, repo, fresh); sha(got) != "m1" {
		t.Errorf("fresh branch's baseline = %s, want main's m1, skipping its own commit x1", sha(got))
	}
}
