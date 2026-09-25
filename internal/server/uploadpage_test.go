package server

import (
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

func TestUncoveredRanges(t *testing.T) {
	b := func(start, end, stmts, count int) profile.Block {
		return profile.Block{StartLine: start, EndLine: end, NumStmts: stmts, Count: count}
	}
	tests := []struct {
		name   string
		blocks []profile.Block
		want   string
	}{
		{"fully covered", []profile.Block{b(1, 5, 3, 2)}, ""},
		{"single line", []profile.Block{b(7, 7, 1, 0)}, "7"},
		{"range", []profile.Block{b(10, 14, 3, 0)}, "10-14"},
		{"adjacent blocks merge", []profile.Block{b(10, 12, 2, 0), b(13, 15, 2, 0)}, "10-15"},
		{"overlapping blocks merge", []profile.Block{b(10, 14, 2, 0), b(12, 16, 2, 0)}, "10-16"},
		{"mixed with covered", []profile.Block{b(1, 5, 3, 9), b(8, 9, 2, 0), b(20, 20, 1, 0)}, "8-9, 20"},
		{"unsorted input", []profile.Block{b(20, 22, 1, 0), b(3, 4, 1, 0)}, "3-4, 20-22"},
		{"zero statement blocks ignored", []profile.Block{b(5, 6, 0, 0)}, ""},
		{
			"capped with more marker",
			[]profile.Block{b(1, 1, 1, 0), b(3, 3, 1, 0), b(5, 5, 1, 0), b(7, 7, 1, 0),
				b(9, 9, 1, 0), b(11, 11, 1, 0), b(13, 13, 1, 0), b(15, 15, 1, 0)},
			"1, 3, 5, 7, 9, 11, +2 more",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uncoveredRanges(tt.blocks); got != tt.want {
				t.Errorf("uncoveredRanges() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewlyUncovered(t *testing.T) {
	b := func(start, end, stmts, count int) profile.Block {
		return profile.Block{StartLine: start, EndLine: end, NumStmts: stmts, Count: count}
	}
	tests := []struct {
		name      string
		cur, base []profile.Block
		want      string
	}{
		{"nothing regressed", []profile.Block{b(1, 5, 2, 1)}, []profile.Block{b(1, 5, 2, 1)}, ""},
		{"whole block regressed", []profile.Block{b(3, 6, 2, 0)}, []profile.Block{b(3, 6, 2, 4)}, "3-6"},
		{"only lines hit before", []profile.Block{b(1, 10, 2, 0)}, []profile.Block{b(4, 5, 1, 1), b(8, 8, 1, 2)}, "4-5, 8"},
		{"hit by an overlapping block now", []profile.Block{b(1, 10, 2, 0), b(3, 4, 1, 1)}, []profile.Block{b(1, 10, 2, 1)}, "1-2, 5-10"},
		{"zero statement blocks ignored", []profile.Block{b(1, 5, 0, 0)}, []profile.Block{b(1, 5, 2, 1)}, ""},
		{"baseline zero statement hit ignored", []profile.Block{b(1, 5, 2, 0)}, []profile.Block{b(1, 5, 0, 1)}, ""},
		{"lines below 1 dropped", []profile.Block{b(-3, 2, 1, 0)}, []profile.Block{b(-3, 2, 1, 1)}, "1-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newlyUncovered(tt.cur, tt.base); got != tt.want {
				t.Errorf("newlyUncovered() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNewlyUncoveredMatchesLineByLineRule checks the span arithmetic against
// the plain per-line definition on many small random block sets.
func TestNewlyUncoveredMatchesLineByLineRule(t *testing.T) {
	lineByLine := func(cur, base []profile.Block) string {
		exec, hit, baseHit := map[int]bool{}, map[int]bool{}, map[int]bool{}
		mark := func(blocks []profile.Block, exec, hit map[int]bool) {
			for _, b := range blocks {
				for l := max(b.StartLine, 1); l <= b.EndLine && b.NumStmts > 0; l++ {
					if exec != nil {
						exec[l] = true
					}
					if b.Count > 0 {
						hit[l] = true
					}
				}
			}
		}
		mark(cur, exec, hit)
		mark(base, nil, baseHit)
		var lines []int
		for l := range exec {
			if !hit[l] && baseHit[l] {
				lines = append(lines, l)
			}
		}
		slices.Sort(lines)
		return diffcov.Ranges(lines)
	}
	rng := rand.New(rand.NewPCG(1, 2))
	blocks := func() []profile.Block {
		out := make([]profile.Block, rng.IntN(6))
		for i := range out {
			start := rng.IntN(40) - 2
			out[i] = profile.Block{StartLine: start, EndLine: start + rng.IntN(8) - 1, NumStmts: rng.IntN(2), Count: rng.IntN(2)}
		}
		return out
	}
	for range 2000 {
		cur, base := blocks(), blocks()
		if got, want := newlyUncovered(cur, base), lineByLine(cur, base); got != want {
			t.Fatalf("newlyUncovered(%v, %v) = %q, want %q", cur, base, got, want)
		}
	}
}

// TestNewlyUncoveredIgnoresDeclaredSpan guards the report pages against
// stored blocks that claim millions of lines: the work must follow the
// number of blocks, not the lines they declare. Expanded line by line this
// input is billions of iterations and would time the test out.
func TestNewlyUncoveredIgnoresDeclaredSpan(t *testing.T) {
	var cur, base []profile.Block
	for col := range 500 {
		cur = append(cur, profile.Block{StartLine: 1, StartCol: col, EndLine: 5_000_000, NumStmts: 1, Count: 0})
		base = append(base, profile.Block{StartLine: 1, StartCol: col, EndLine: 5_000_000, NumStmts: 1, Count: 1})
	}
	if got := newlyUncovered(cur, base); got != "1-5000000" {
		t.Errorf("newlyUncovered() = %q, want %q", got, "1-5000000")
	}
}

// testProfileFull covers a.go's 7-9 block that testProfile leaves uncovered,
// so a following testProfile upload reads as a regression: a.go drops
// 100% -> 75% and lines 7-9 become newly uncovered.

const testProfileFull = `mode: set
example.com/m/a.go:1.1,5.2 6 1
example.com/m/a.go:7.1,9.2 2 1
example.com/m/b.go:1.1,3.2 2 1
`

// The files card's before → after column and the regression it names: a
// baseline upload covers a.go fully, the head upload drops it to 75% and
// leaves lines 7-9 uncovered.
func TestAPIUploadPageBeforeAfter(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "base1", "branch": "main"}, testProfileFull)
	doUpload(t, f, "secret-token", map[string]string{"commit": "head1", "branch": "main"}, testProfile)

	got := decodeJSON[uploadPageDTO](t, get(f, "/api/ui/uploads/2"))
	if got.Files == nil || !got.Files.HasBase {
		t.Fatalf("files = %+v, want a baseline to compare against", got.Files)
	}
	byPath := map[string]fileRowDTO{}
	for _, row := range got.Files.Files {
		byPath[row.Path] = row
	}
	a := byPath["example.com/m/a.go"]
	if a.Before == nil || *a.Before != 100 || a.Coverage != 75 {
		t.Errorf("a.go = %+v, want 100%% before and 75%% now", a)
	}
	if a.NewlyUncovered != "7-9" || !a.CoverageChanged {
		t.Errorf("a.go regression = %+v, want lines 7-9 newly uncovered", a)
	}
	// The baseline's own counts ride along, so the app's directory rollup
	// weighs a.go by what it had then (8 of 8), not by what it has now.
	if a.BeforeCoveredStmts == nil || a.BeforeTotalStmts == nil || *a.BeforeCoveredStmts != 8 || *a.BeforeTotalStmts != 8 {
		t.Errorf("a.go baseline statements = %v/%v, want 8 of 8", a.BeforeCoveredStmts, a.BeforeTotalStmts)
	}
	// An unchanged file is still listed, and says it did not move.
	if b := byPath["example.com/m/b.go"]; b.CoverageChanged || b.NewFile {
		t.Errorf("b.go = %+v, want it listed as unchanged", b)
	}
	// The first upload has nothing before it: all three baseline fields are null.
	first := decodeJSON[uploadPageDTO](t, get(f, "/api/ui/uploads/1"))
	for _, row := range first.Files.Files {
		if row.Before != nil || row.BeforeCoveredStmts != nil || row.BeforeTotalStmts != nil {
			t.Errorf("%s on the first upload = %+v, want no baseline fields", row.Path, row)
		}
	}
}

// A PR build on a branch with no upload of its own is still baselined —
// against the default branch, the state it would merge into.
func TestAPIUploadPagePRBaselinesAgainstDefaultBranch(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "main1", "branch": "main"}, testProfileFull)
	doUpload(t, f, "secret-token", map[string]string{"commit": "pr1", "branch": "feature/x", "pr_id": "7"}, testProfile)

	got := decodeJSON[uploadPageDTO](t, get(f, "/api/ui/uploads/2"))
	if got.Files == nil || !got.Files.HasBase {
		t.Fatalf("files = %+v, want main's upload as the baseline", got.Files)
	}
	for _, row := range got.Files.Files {
		if row.Path == "example.com/m/a.go" && (row.Before == nil || *row.Before != 100) {
			t.Errorf("a.go = %+v, want 100%% at the main baseline", row)
		}
	}
	if got.Verdict.Base == nil || got.Verdict.Base.UploadID != 1 {
		t.Errorf("base = %+v, want main's upload", got.Verdict.Base)
	}
}

// The provenance card: how the upload arrived, as the CLI and the action
// reported it.
func TestAPIUploadPageProvenance(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{
		"commit": "c1", "branch": "main",
		"commit_message": "Fix the ledger reconcile",
		"commit_author":  "Ada Lovelace",
		"uploader":       "gocov v1.2.3",
		"uploader_kind":  "action",
		"ci_provider":    "github",
		"ci_run_url":     "https://github.com/acme/widgets/actions/runs/7",
	}, testProfile)

	got := decodeJSON[uploadPageDTO](t, get(f, "/api/ui/uploads/1"))
	if got.Upload.CommitMessage != "Fix the ledger reconcile" || got.Upload.CommitAuthor != "Ada Lovelace" {
		t.Errorf("commit = %+v", got.Upload)
	}
	prov := got.Provenance
	if prov.CILabel != "GitHub Actions" || prov.CIRunURL != "https://github.com/acme/widgets/actions/runs/7" {
		t.Errorf("CI = %+v", prov)
	}
	if prov.Uploader != "gocov v1.2.3" || prov.UploaderKind != "Action" {
		t.Errorf("uploader = %+v", prov)
	}
	if prov.ProfileName != "coverage.out" || prov.Format != "go" {
		t.Errorf("profile = %+v", prov)
	}
}

func TestUploadProfileDownload(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)

	rec := get(f, "/uploads/1/profile")
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "coverage.out") {
		t.Errorf("content-disposition = %q", cd)
	}
	if rec.Body.String() != testProfile {
		t.Errorf("download body does not match the uploaded profile:\n%s", rec.Body.String())
	}
	if rec := get(f, "/uploads/999/profile"); rec.Code != http.StatusNotFound {
		t.Errorf("missing upload profile: code = %d, want 404", rec.Code)
	}
}

// A file the PR's diff touches is flagged as source-changed, which is
// what the files card filters on.
func TestAPIUploadPageDiffCoverageSourceChanged(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	f.forge.DiffText = testPRDiff

	doUpload(t, f, "secret-token", map[string]string{"commit": "base1", "branch": "main"}, testProfileFull)
	doUpload(t, f, "secret-token", map[string]string{
		"commit": "prcommit1", "branch": "feature/x", "pr_id": "42",
	}, testProfile)

	got := decodeJSON[uploadPageDTO](t, get(f, "/api/ui/uploads/2"))
	if got.Files == nil {
		t.Fatal("no files card")
	}
	changed := 0
	for _, row := range got.Files.Files {
		if row.SourceChanged {
			changed++
		}
	}
	if changed != 1 {
		t.Errorf("source-changed files = %d, want the diff's one file: %+v", changed, got.Files.Files)
	}
	if got.Diff == nil || got.Diff.TotalLines == 0 {
		t.Errorf("diff coverage = %+v, want the PR's measured diff", got.Diff)
	}
}

// fakeProvider is an auth.Provider whose Identity is canned.

func TestAPIUploadPage(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"},
		"mode: set\nexample.com/m/a.go:1.1,5.2 10 3\n") // 100% baseline
	doUpload(t, f, "secret-token", map[string]string{
		"commit": "c2", "branch": "main", "commit_message": "fix the thing", "commit_author": "Jane Dev",
	}, testProfile)

	got := decodeJSON[uploadPageDTO](t, get(f, "/api/ui/uploads/2"))
	if got.Repo.Slug != "acme/widgets" {
		t.Errorf("repo = %+v", got.Repo)
	}
	if got.Upload.ID != 2 || got.Upload.SHA != "c2" || got.Upload.Branch != "main" {
		t.Errorf("upload = %+v", got.Upload)
	}
	if got.Upload.CommitMessage != "fix the thing" || got.Upload.CommitAuthor != "Jane Dev" {
		t.Errorf("commit metadata = %+v", got.Upload)
	}
	if got.Upload.Tokenless {
		t.Error("a token-authenticated upload reported as tokenless")
	}
	// No gate is configured, so the verdict is neutral — but the numbers
	// against the baseline are still there.
	if got.Verdict.State != "neutral" || got.Verdict.Coverage != 80 {
		t.Errorf("verdict = %+v", got.Verdict)
	}
	if got.Verdict.Delta == nil || *got.Verdict.Delta != -20 {
		t.Errorf("delta = %v, want -20", got.Verdict.Delta)
	}
	if got.Verdict.Base == nil || got.Verdict.Base.UploadID != 1 {
		t.Errorf("base = %+v, want the first upload", got.Verdict.Base)
	}
	if got.CoveredStmts != 8 || got.TotalStmts != 10 || len(got.Files.Files) != 2 {
		t.Errorf("totals = %d/%d over %d files", got.CoveredStmts, got.TotalStmts, len(got.Files.Files))
	}
	if got.Provenance.Format != "go" {
		t.Errorf("format = %q", got.Provenance.Format)
	}
	if got.Diff != nil {
		t.Errorf("diff = %+v, want none for a branch build", got.Diff)
	}
	if got.Files == nil || len(got.Files.Files) != 2 || got.Files.UploadID != 2 {
		t.Fatalf("files = %+v", got.Files)
	}
	if got.Provenance.ProfileName != "coverage.out" || got.Provenance.PartsNote == "" {
		t.Errorf("provenance = %+v", got.Provenance)
	}
	if got.Provenance.ReceivedAt.IsZero() {
		t.Error("provenance carries no received time")
	}
	if got.DownloadURL == nil || *got.DownloadURL != "/uploads/2/profile" {
		t.Errorf("download url = %v", got.DownloadURL)
	}
}

// An upload whose raw profile was never stored offers no download.
func TestAPIUploadPageWithoutProfile(t *testing.T) {
	f := newFixture(t, nil)
	u := &store.Upload{RepoID: f.repo.ID, CommitSHA: "c1", Branch: "main", Format: "go", TotalPct: 80, Part: "default"}
	if err := f.store.CreateUpload(t.Context(), u, nil); err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[uploadPageDTO](t, get(f, "/api/ui/uploads/1"))
	if got.DownloadURL != nil {
		t.Errorf("download url = %v, want null", *got.DownloadURL)
	}
	if got.Files == nil || len(got.Files.Files) != 0 {
		t.Errorf("files = %+v, want an empty list", got.Files)
	}
}

// On a feature branch the page's baseline (the branch's own previous
// build) and the gate's drop baseline (the default branch) differ. The
// verdict's reason must narrate the comparison the gate made, never a drop
// against the branch's history that the gate did not judge.
func TestVerdictReasonNarratesTheGatesDropBaseline(t *testing.T) {
	f := newFixture(t, nil)
	f.repo.Gate = store.Gate{MaxCoverageDrop: new(float64(2))}
	if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
		t.Fatal(err)
	}
	half := "mode: set\nexample.com/m/a.go:1.1,5.2 5 1\nexample.com/m/a.go:7.1,9.2 5 0\n"
	full := "mode: set\nexample.com/m/a.go:1.1,5.2 10 3\n"
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, half)        // 50%
	doUpload(t, f, "secret-token", map[string]string{"commit": "f1", "branch": "feat"}, full)        // 100%
	doUpload(t, f, "secret-token", map[string]string{"commit": "f2", "branch": "feat"}, testProfile) // 80%

	// 80% is 20 points under the branch's previous build but 30 above the
	// default branch, which is what the drop rule compares against.
	for _, path := range []string{"/api/ui/uploads/3", "/api/ui/repos/bitbucket/acme/widgets?branch=feat"} {
		var v verdictDTO
		if path == "/api/ui/uploads/3" {
			v = decodeJSON[uploadPageDTO](t, get(f, path)).Verdict
		} else {
			v = decodeJSON[repoPageDTO](t, get(f, path)).Summary.Verdict
		}
		if v.State != "pass" {
			t.Errorf("%s: state = %q, want pass", path, v.State)
		}
		if want := "Coverage held or rose against the default branch."; v.Reason != want {
			t.Errorf("%s: reason = %q, want %q", path, v.Reason, want)
		}
	}

	up, err := f.store.Upload(t.Context(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if up.GateBasePct == nil || *up.GateBasePct != 50 {
		t.Errorf("upload GateBasePct = %v, want the default branch's 50", up.GateBasePct)
	}
}

// Pages describe an upload against the gate it was judged by: editing or
// removing the gate afterwards does not rewrite its verdict, its reason,
// its row in the history or the dashboard's notice.
func TestVerdictsKeepTheGateTheyWereJudgedBy(t *testing.T) {
	f := newFixture(t, nil)
	setGate := func(g store.Gate) {
		t.Helper()
		f.repo.Gate = g
		if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
			t.Fatal(err)
		}
	}
	setGate(store.Gate{MinCoverage: new(float64(90))})
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile) // 80%: fails 90

	for _, later := range []store.Gate{{MinCoverage: new(float64(50))}, {}} {
		setGate(later) // 80% would pass the first, and the second has no rules at all
		upload := decodeJSON[uploadPageDTO](t, get(f, "/api/ui/uploads/1"))
		repo := decodeJSON[repoPageDTO](t, get(f, "/api/ui/repos/bitbucket/acme/widgets"))
		for name, v := range map[string]verdictDTO{"upload page": upload.Verdict, "repo page": repo.Summary.Verdict} {
			if v.State != "fail" || !strings.Contains(v.Reason, "below the minimum of 90%") {
				t.Errorf("gate now %+v, %s verdict = %q / %q; want fail, below the minimum of 90%%", later, name, v.State, v.Reason)
			}
		}
		if got := repo.Uploads[0].Gate; got != "fail" {
			t.Errorf("gate now %+v, history row gate = %q, want fail", later, got)
		}
		dash := decodeJSON[dashboardDTO](t, get(f, "/api/ui/dashboard"))
		if len(dash.Repos) != 1 || dash.Repos[0].Gate != "fail" {
			t.Errorf("gate now %+v, dashboard row = %+v, want fail", later, dash.Repos)
		}
		if len(dash.Attention) != 1 || dash.Attention[0].MinCoverage == nil || *dash.Attention[0].MinCoverage != 90 {
			t.Errorf("gate now %+v, notice = %+v, want failing below the judged 90%% minimum", later, dash.Attention)
		}
	}

	// An upload judged with no gate set is not "passed".
	doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "branch": "main"}, testProfile)
	if got := decodeJSON[repoPageDTO](t, get(f, "/api/ui/repos/bitbucket/acme/widgets")).Uploads[0].Gate; got != "none" {
		t.Errorf("ungated upload's row gate = %q, want none", got)
	}
}

// A gate that failed on another rule does not claim the total was below
// the minimum.
func TestFailingNoticeQuotesTheMinimumOnlyWhenItFailed(t *testing.T) {
	f := newFixture(t, nil)
	f.repo.Gate = store.Gate{MinCoverage: new(float64(50)), MaxCoverageDrop: new(float64(1))}
	if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
		t.Fatal(err)
	}
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile) // 80%
	doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "branch": "main"},
		"mode: set\nexample.com/m/a.go:1.1,5.2 7 1\nexample.com/m/a.go:7.1,9.2 3 0\n") // 70%: a 10-point drop

	dash := decodeJSON[dashboardDTO](t, get(f, "/api/ui/dashboard"))
	if len(dash.Attention) != 1 || dash.Attention[0].Kind != "failing" {
		t.Fatalf("notices = %+v, want one failing", dash.Attention)
	}
	if got := dash.Attention[0].MinCoverage; got != nil {
		t.Errorf("notice quotes a %v%% minimum; 70%% is above it — the drop rule failed", *got)
	}
}
