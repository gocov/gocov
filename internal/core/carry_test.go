package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// recompute is Recompute that fails the test on error.
func recompute(t *testing.T, p *Pipeline, repo *store.Repo, u *store.Upload) *Merged {
	t.Helper()
	m, err := p.Recompute(t.Context(), repo, u)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// addDiffPart is addBranchPart for a build of PR 7 on branch feat, with
// covered of 10 statements and the given diff coverage.
func addDiffPart(t *testing.T, st *storemem.Store, repo *store.Repo, commit, part string, covered int64, diff *diffcov.Result) *store.Upload {
	t.Helper()
	return storePart(t, st, &store.Upload{
		RepoID: repo.ID, CommitSHA: commit, Branch: "feat", PRID: "7", Format: "go", Part: part,
		CoveredStmts: covered, TotalStmts: 10, DiffCoverage: diff,
	})
}

// storedReport reads a commit's merged report back.
func storedReport(t *testing.T, st *storemem.Store, repo *store.Repo, commit string) *store.CommitReport {
	t.Helper()
	cr, err := st.CommitReport(t.Context(), repo.ID, commit)
	if err != nil {
		t.Fatal(err)
	}
	return cr
}

func TestRecomputeCarriesANamedPartForward(t *testing.T) {
	p, st, repo := newPipeline(t, store.Gate{})
	repo.CarryforwardParts = []string{"api"}

	api1 := addPart(t, st, repo, "c1", "api", 8, 10)
	recompute(t, p, repo, api1)
	recompute(t, p, repo, addPart(t, st, repo, "c1", "web", 5, 10))

	// c2 changed only the web: its api is c1's.
	m := recompute(t, p, repo, addPart(t, st, repo, "c2", "web", 6, 10))
	if m.Upload.CoveredStmts != 14 || m.Upload.TotalStmts != 20 {
		t.Fatalf("c2 = %d/%d, want 14/20 with c1's api carried", m.Upload.CoveredStmts, m.Upload.TotalStmts)
	}
	if got := strings.Join(m.Parts, ","); got != "api,web" {
		t.Errorf("parts = %q, want api,web", got)
	}
	want := []CarriedPart{{Part: "api", CommitSHA: "c1", UploadID: api1.ID}}
	if !slices.Equal(m.Carried, want) {
		t.Errorf("carried = %+v, want %+v", m.Carried, want)
	}
	cr := storedReport(t, st, repo, "c2")
	if cr.PartCount != 2 || !slices.Equal(cr.CarriedUploadIDs, []int64{api1.ID}) || cr.TotalPct != 70 {
		t.Errorf("c2's report: %d parts, carried %v, %.1f%%; want 2, [%d], 70%%", cr.PartCount, cr.CarriedUploadIDs, cr.TotalPct, api1.ID)
	}

	// A chain ends at the upload that measured the part: c3 takes c1's
	// api through c2's report.
	m = recompute(t, p, repo, addPart(t, st, repo, "c3", "web", 7, 10))
	if !slices.Equal(m.Carried, want) {
		t.Errorf("c3 carried = %+v, want c1's api %+v", m.Carried, want)
	}

	// c2's own api, landing late, replaces the carried one.
	m = recompute(t, p, repo, addPart(t, st, repo, "c2", "api", 9, 10))
	if m.Carried != nil || m.Upload.CoveredStmts != 15 {
		t.Errorf("c2 with its own api: carried %+v, %d covered; want none, 15", m.Carried, m.Upload.CoveredStmts)
	}
	if cr := storedReport(t, st, repo, "c2"); cr.CarriedUploadIDs != nil {
		t.Errorf("c2's report still carries %v", cr.CarriedUploadIDs)
	}
}

func TestRecomputeCarriesOnlyTheNamedParts(t *testing.T) {
	p, st, repo := newPipeline(t, store.Gate{})
	repo.CarryforwardParts = []string{"api", "docs"} // docs never uploads anywhere

	for _, part := range []string{"api", "web", "e2e"} {
		recompute(t, p, repo, addPart(t, st, repo, "c1", part, 5, 10))
	}
	m := recompute(t, p, repo, addPart(t, st, repo, "c2", "web", 10, 10))
	if got := strings.Join(m.Parts, ","); got != "api,web" {
		t.Errorf("parts = %q, want api,web: e2e is not carried, docs has nothing to carry", got)
	}
	if m.Upload.CoveredStmts != 15 || m.Upload.TotalStmts != 20 {
		t.Errorf("c2 = %d/%d, want 15/20", m.Upload.CoveredStmts, m.Upload.TotalStmts)
	}

	// Without the setting nothing is carried.
	repo.CarryforwardParts = nil
	if m := recompute(t, p, repo, addPart(t, st, repo, "c3", "web", 10, 10)); m.Carried != nil || len(m.Parts) != 1 {
		t.Errorf("no carryforward: parts %v, carried %+v", m.Parts, m.Carried)
	}
}

// A commit takes its parts from what came before it was first seen, so a
// CI re-run on an old commit never borrows from a newer one.
func TestRecomputeCarriesOnlyFromEarlierCommits(t *testing.T) {
	p, st, repo := newPipeline(t, store.Gate{})
	repo.CarryforwardParts = []string{"api"}

	api1 := addPart(t, st, repo, "c1", "api", 8, 10)
	recompute(t, p, repo, api1)
	recompute(t, p, repo, addPart(t, st, repo, "c2", "web", 5, 10))
	recompute(t, p, repo, addPart(t, st, repo, "c3", "api", 1, 10))

	m := recompute(t, p, repo, addPart(t, st, repo, "c2", "web", 6, 10)) // the retry
	want := []CarriedPart{{Part: "api", CommitSHA: "c1", UploadID: api1.ID}}
	if !slices.Equal(m.Carried, want) {
		t.Errorf("re-run of c2 carried %+v, want c1's api %+v, never c3's", m.Carried, want)
	}
}

func TestRecomputeCarriesFromTheDefaultBranchForANewBranch(t *testing.T) {
	p, st, repo := newPipeline(t, store.Gate{})
	repo.CarryforwardParts = []string{"api"}

	api1 := addPart(t, st, repo, "m1", "api", 8, 10)
	recompute(t, p, repo, api1)
	recompute(t, p, repo, addPart(t, st, repo, "m1", "web", 5, 10))

	// The branch's first build has no history of its own: main's api.
	m := recompute(t, p, repo, addBranchPart(t, st, repo, "f1", "feat", "7", "web", 6, 10))
	want := []CarriedPart{{Part: "api", CommitSHA: "m1", UploadID: api1.ID}}
	if !slices.Equal(m.Carried, want) {
		t.Fatalf("f1 carried %+v, want main's api %+v", m.Carried, want)
	}
	// Its next build finds it on the branch itself, through f1's report.
	m = recompute(t, p, repo, addBranchPart(t, st, repo, "f2", "feat", "7", "web", 7, 10))
	if !slices.Equal(m.Carried, want) {
		t.Errorf("f2 carried %+v, want %+v", m.Carried, want)
	}
	// A fork's PR under main's name is never main's history.
	recompute(t, p, repo, addBranchPart(t, st, repo, "k1", "main", "9", "api", 0, 10))
	if m := recompute(t, p, repo, addPart(t, st, repo, "m2", "web", 5, 10)); !slices.Equal(m.Carried, want) {
		t.Errorf("m2 carried %+v, want m1's api %+v, not the fork's", m.Carried, want)
	}
}

// A carried part feeds the totals, so the comparisons and the gate see
// the same parts on both sides — but its diff coverage is another
// commit's, so the merged diff is the commit's own parts' alone.
func TestRecomputeCarriedPartsInTheGateButNotTheDiff(t *testing.T) {
	drop := 1.0
	p, st, repo := newPipeline(t, store.Gate{MaxCoverageDrop: &drop})
	repo.CarryforwardParts = []string{"api"}

	recompute(t, p, repo, addPart(t, st, repo, "m1", "api", 8, 10))
	recompute(t, p, repo, addPart(t, st, repo, "m1", "web", 5, 10))

	// An api build from an earlier PR, with its own diff, on the branch.
	recompute(t, p, repo, addDiffPart(t, st, repo, "f1", "api", 8,
		&diffcov.Result{TotalLines: 4, Files: []diffcov.FileCoverage{{Path: "api.go", TotalLines: 4}}}))
	m := recompute(t, p, repo, addDiffPart(t, st, repo, "f2", "web", 5,
		&diffcov.Result{TotalLines: 2, CoveredLines: 2, Files: []diffcov.FileCoverage{{Path: "web.go", TotalLines: 2, CoveredLines: 2}}}))
	if m.Upload.DiffCoverage == nil || m.Upload.DiffCoverage.TotalLines != 2 || m.Upload.DiffCoverage.CoveredLines != 2 {
		t.Errorf("merged diff = %+v, want web's alone (2/2)", m.Upload.DiffCoverage)
	}
	if m.Verdict.Failed() {
		t.Errorf("gate failed with the api carried: %v", m.Verdict.Failures)
	}
	if m.Delta == nil || *m.Delta != 0 {
		t.Errorf("delta = %v, want 0 against main's api+web", m.Delta)
	}
}

func TestPRCommentNamesCarriedParts(t *testing.T) {
	p, _, _ := newPipeline(t, store.Gate{})
	body := p.prCommentBody(&store.Upload{CommitSHA: "f2f2f2f2f2", TotalPct: 70}, "", nil, Verdict{}, []CarriedPart{
		{Part: "api", CommitSHA: "a1a1a1a1a1a1a1a1", UploadID: 3},
		{Part: "e2e", CommitSHA: "b2b2b2b2b2b2b2b2", UploadID: 4},
	})
	if want := "- Carried forward: `api` from `a1a1a1a1a1a1`, `e2e` from `b2b2b2b2b2b2`\n"; !strings.Contains(body, want) {
		t.Errorf("comment lacks %q:\n%s", want, body)
	}
	if body := p.prCommentBody(&store.Upload{CommitSHA: "f2f2f2f2f2"}, "", nil, Verdict{}, nil); strings.Contains(body, "Carried") {
		t.Errorf("comment names carried parts without any:\n%s", body)
	}
}
