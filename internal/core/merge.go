// Merging the parts of one commit. A commit's coverage may arrive as
// several uploads (backend, frontend, e2e, ...), and every upload
// rebuilds the commit's merged report from the latest upload of every
// part — that merged view, not the individual upload, is what the
// response, the gate and every forge surface are driven from.

package core

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// Recompute rebuilds the merged report for the upload's commit
// from the latest upload of every part — and the parts the repo carries
// forward that the commit did not upload (carryForward) — persists it, and returns the merged
// view that drives the response and the forge side effects. It is
// self-healing: because every upload recomputes the whole commit, a partial
// early state (only the backend part in, say) is corrected in place as the
// remaining parts arrive. The trade-off is a window in which the merged
// numbers are incomplete — see the note on merged reports in docs/parts.md.
//
// The returned upload is synthetic: it carries the merged totals and diff
// coverage to the existing push helpers, with the triggering upload's id so
// the report card and PR comment link back to it.
type Merged struct {
	Upload   *store.Upload
	Delta    *float64
	Verdict  Verdict
	Warnings []string // surfaced to the uploader, e.g. conservative diff merges
	// Parts names, sorted, every part the merged report was built from —
	// the parts received so far, since gocov never waits for a fixed set,
	// and those carried forward from earlier commits.
	Parts []string
	// Carried are the parts among Parts taken from earlier commits
	// (carryForward), sorted by name; nil when none was.
	Carried []CarriedPart
}

// uploadIDs is the ids of the given uploads, nil for none.
func uploadIDs(ups []*store.Upload) []int64 {
	var ids []int64
	for _, u := range ups {
		ids = append(ids, u.ID)
	}
	return ids
}

// recomputeTimeout bounds a single recompute so a saturated connection pool
// fails the upload fast instead of hanging a CI client indefinitely.
const recomputeTimeout = 30 * time.Second

func (p *Pipeline) Recompute(ctx context.Context, repo *store.Repo, u *store.Upload) (*Merged, error) {
	ctx, cancel := context.WithTimeout(ctx, recomputeTimeout)
	defer cancel()

	// The whole recompute — read every part, merge, upsert — runs inside one
	// locked transaction, serialized per commit against concurrent uploads
	// (parallel CI jobs are the point) so it cannot interleave with or
	// clobber a newer recompute and drop a part.
	var result *Merged
	err := p.Store.WithCommitReportTx(ctx, repo.ID, u.CommitSHA, func(ctx context.Context, tx store.CommitTx) error {
		own, err := tx.LatestUploadsPerPart(ctx, repo.ID, u.CommitSHA)
		if err != nil {
			return fmt.Errorf("loading commit parts: %w", err)
		}

		// A commit the default branch built itself (no PR) stays that
		// branch's, whatever branch or PR uploads it next — one cut from it,
		// a fast-forward, a PR whose head it is — or it would drop out of the
		// default branch's trend, dashboard row, badge and baselines. The
		// later upload brings its numbers, but a PR's diff coverage is that
		// PR's alone: the report keeps none, records the verdict without it,
		// and links to the branch's own build, so the pages it opens agree.
		// The delta and the forge surfaces speak for this upload — its
		// branch, its PR, its diff — so they keep u's, and so does the page
		// they link to (MergedReportFor).
		var keptBuild int64
		if !defaultBuild(repo, u.Branch, u.PRID) {
			if keptBuild, err = tx.BranchBuild(ctx, repo.ID, u.CommitSHA, repo.DefaultBranch); err != nil {
				return fmt.Errorf("checking the default branch's builds: %w", err)
			}
		}
		reportBranch, reportPR, reportUpload := u.Branch, u.PRID, u.ID
		if keptBuild != 0 {
			reportBranch, reportPR, reportUpload = repo.DefaultBranch, "", keptBuild
		}
		// The parts the commit did not upload come from the history of the
		// branch its report stays on.
		carried, err := carryForward(ctx, tx, repo, u.CommitSHA, reportBranch, own)
		if err != nil {
			return err
		}
		parts := slices.Concat(own, carried)

		names := make([]string, 0, len(parts))
		for _, p := range parts {
			names = append(names, p.Part)
		}
		covered, total, err := mergedCoverage(ctx, tx, parts)
		if err != nil {
			return err
		}
		totalPct := profile.Percent(covered, total)
		// A carried part's diff coverage was measured against another
		// commit's diff, so only the commit's own parts give its diff.
		mergedDiff, diffConflicts := partsDiff(own)
		var warnings []string
		if len(diffConflicts) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"diff coverage merged conservatively for %d changed file(s) whose parts disagree on their changed lines (%s); merged coverage is a safe lower bound",
				len(diffConflicts), strings.Join(diffConflicts, ", ")))
		}

		var deltaPct *float64
		prev, err := deltaBase(ctx, tx, repo, u.Branch, u.CommitSHA, 0, names)
		if err != nil {
			return err
		}
		if prev != nil {
			prevPct, err := prev.totalOver(ctx, tx, names)
			if err != nil {
				return fmt.Errorf("loading baseline report: %w", err)
			}
			deltaPct = new(totalPct - prevPct)
		}

		dropBase, err := gateDropBase(ctx, tx, repo, u.CommitSHA, names)
		if err != nil {
			return err
		}

		gate := EvaluateGate(repo.Gate, totalPct, dropBase, mergedDiff)

		cr := &store.CommitReport{
			RepoID:           repo.ID,
			CommitSHA:        u.CommitSHA,
			Branch:           reportBranch,
			PRID:             reportPR,
			TotalPct:         totalPct,
			CoveredStmts:     covered,
			TotalStmts:       total,
			GateFailed:       gate.Failed(),
			GateBasePct:      dropBase,
			Gate:             new(repo.Gate),
			DiffCoverage:     mergedDiff,
			PartCount:        len(parts),
			UploadID:         reportUpload,
			CarriedUploadIDs: uploadIDs(carried),
		}
		if keptBuild != 0 {
			setDiff(repo, cr, nil)
		}
		if err := tx.UpsertCommitReport(ctx, cr); err != nil {
			return fmt.Errorf("saving merged report: %w", err)
		}

		mergedUpload := &store.Upload{
			ID:           u.ID,
			RepoID:       repo.ID,
			CommitSHA:    u.CommitSHA,
			Branch:       u.Branch,
			PRID:         u.PRID,
			TotalPct:     totalPct,
			CoveredStmts: covered,
			TotalStmts:   total,
			DiffCoverage: mergedDiff,
		}
		slices.Sort(names)
		result = &Merged{
			Upload:   mergedUpload,
			Delta:    deltaPct,
			Verdict:  gate,
			Warnings: warnings,
			Parts:    names,
			Carried:  CarriedParts(u.CommitSHA, parts),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// defaultBuild reports whether branch and prID name a build of the repo's
// default branch itself, not a PR's.
func defaultBuild(repo *store.Repo, branch, prID string) bool {
	return branch == repo.DefaultBranch && prID == ""
}

// partsDiff merges the diff coverage of a commit's parts, naming the
// changed files they disagree on (diffcov.Merge); nil when none has any.
func partsDiff(parts []*store.Upload) (*diffcov.Result, []string) {
	diffs := make([]*diffcov.Result, 0, len(parts))
	for _, p := range parts {
		if p.DiffCoverage != nil {
			diffs = append(diffs, p.DiffCoverage)
		}
	}
	return diffcov.Merge(diffs...)
}

// setDiff sets a report's diff coverage and the gate verdict it gives,
// judged by the gate and drop baseline the report was judged with.
func setDiff(repo *store.Repo, cr *store.CommitReport, diff *diffcov.Result) {
	cr.DiffCoverage = diff
	cr.GateFailed = EvaluateGate(store.JudgedGate(cr.Gate, repo.Gate), cr.TotalPct, cr.GateBasePct, diff).Failed()
}

// latestParts is the one read MergedReportFor and MergedReportForPR need.
type latestParts interface {
	LatestUploadsPerPart(ctx context.Context, repoID int64, commitSHA string) ([]*store.Upload, error)
}

// MergedReportFor is a commit's merged report as upload u's build states
// it — the page its forge surfaces link to: the stored report, with the
// diff coverage of the commit's parts and its verdict added back when the
// default branch keeps the commit and u is another branch's or a PR's
// build (Recompute). It keeps the stored branch, and so its baseline.
func MergedReportFor(ctx context.Context, reports latestParts, repo *store.Repo, cr *store.CommitReport, u *store.Upload) (*store.CommitReport, error) {
	if !withholdsDiff(repo, cr, u) {
		return cr, nil
	}
	parts, err := reports.LatestUploadsPerPart(ctx, repo.ID, cr.CommitSHA)
	if err != nil {
		return nil, fmt.Errorf("loading commit parts: %w", err)
	}
	return withPartsDiff(repo, cr, parts), nil
}

// MergedReportForPR is MergedReportFor for a reader that names a pull
// request rather than one of its uploads, as the read API does: the
// commit's merged report as PR prID's build states it, and that build —
// the PR's newest upload among the commit's parts, which names the PR's
// branch and the page its surfaces link to. A PR none of whose uploads is
// among the parts, because it never built the commit or later builds
// replaced every part it uploaded, gets the stored report and no build.
func MergedReportForPR(ctx context.Context, reports latestParts, repo *store.Repo, cr *store.CommitReport, prID string) (*store.CommitReport, *store.Upload, error) {
	if prID == "" {
		return cr, nil, nil
	}
	parts, err := reports.LatestUploadsPerPart(ctx, repo.ID, cr.CommitSHA)
	if err != nil {
		return nil, nil, fmt.Errorf("loading commit parts: %w", err)
	}
	var build *store.Upload
	for _, p := range parts {
		if p.PRID == prID && (build == nil || p.ID > build.ID) {
			build = p
		}
	}
	if build == nil || !withholdsDiff(repo, cr, build) {
		return cr, build, nil
	}
	return withPartsDiff(repo, cr, parts), build, nil
}

// withholdsDiff reports whether cr is stored without the diff coverage u's
// build gave it: the default branch keeps the commit, and u is another
// branch's or a PR's build of it (Recompute).
func withholdsDiff(repo *store.Repo, cr *store.CommitReport, u *store.Upload) bool {
	return defaultBuild(repo, cr.Branch, cr.PRID) && !defaultBuild(repo, u.Branch, u.PRID)
}

// withPartsDiff is cr with the diff coverage of the commit's parts put
// back, and the verdict it gives.
func withPartsDiff(repo *store.Repo, cr *store.CommitReport, parts []*store.Upload) *store.CommitReport {
	view := *cr
	diff, _ := partsDiff(parts)
	setDiff(repo, &view, diff)
	return &view
}

// partFiles is the one read a merge of several parts needs; both the
// store and a commit-report transaction answer it.
type partFiles interface {
	PartFiles(ctx context.Context, uploadIDs []int64) ([]*store.UploadFile, error)
}

// mergedCoverage counts the covered and total statements of the parts
// merged together. A lone part is its own merge (MergeParts), and its
// upload row already carries the totals — the common single-part commit
// reads no files at all. Several parts are read in one query and
// merged, so a block two parts both report counts once.
func mergedCoverage(ctx context.Context, tx partFiles, parts []*store.Upload) (covered, total int64, err error) {
	if len(parts) == 1 {
		return parts[0].CoveredStmts, parts[0].TotalStmts, nil
	}
	files, err := tx.PartFiles(ctx, uploadIDs(parts))
	if err != nil {
		return 0, 0, fmt.Errorf("loading part files: %w", err)
	}
	for _, f := range MergeParts(files) {
		covered += f.CoveredStmts
		total += f.TotalStmts
	}
	return covered, total, nil
}

// MergeParts merges the files of a commit's parts into the files of its
// merged report, ordered by path — the rule its totals are counted by
// (mergedCoverage), for the pages that list those files. A block several
// parts report counts once, covered when any part ran it (profile.Merge);
// each merged file belongs to the newest upload that reported it.
//
// Files all of one upload — the common single-part commit — are their
// own merge when they are canonical, as every parser leaves them (one row
// per path, blocks in profile.Merge's order with no position twice): they
// come back as they are, in path order, without re-merging every block.
// Rows that are not canonical are merged, so the answer never depends on
// the shortcut. The input slice is not reordered either way.
func MergeParts(files []*store.UploadFile) []*store.UploadFile {
	if len(files) > 0 && !slices.ContainsFunc(files, func(f *store.UploadFile) bool { return f.UploadID != files[0].UploadID }) {
		sorted := slices.SortedFunc(slices.Values(files), func(a, b *store.UploadFile) int { return cmp.Compare(a.Path, b.Path) })
		if canonicalFiles(sorted) {
			return sorted
		}
	}
	return mergeAll(files)
}

// canonicalFiles reports whether files sorted by path name each path once
// and keep their blocks canonical (profile.Canonical).
func canonicalFiles(sorted []*store.UploadFile) bool {
	for i, f := range sorted {
		if i > 0 && f.Path == sorted[i-1].Path || !profile.Canonical(f.Blocks) {
			return false
		}
	}
	return true
}

// mergeAll is MergeParts without the shortcut.
func mergeAll(files []*store.UploadFile) []*store.UploadFile {
	owner := make(map[string]int64, len(files))
	all := &profile.Profile{Files: make([]profile.File, 0, len(files))}
	for _, f := range files {
		owner[f.Path] = max(owner[f.Path], f.UploadID)
		all.Files = append(all.Files, profile.File{Path: f.Path, Blocks: f.Blocks})
	}
	merged := profile.Merge(all)
	out := make([]*store.UploadFile, len(merged.Files))
	for i := range merged.Files {
		out[i] = newUploadFile(&merged.Files[i])
		out[i].UploadID = owner[out[i].Path]
	}
	return out
}
