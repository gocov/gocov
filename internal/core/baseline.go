// Comparison baselines: which earlier coverage a commit or upload is
// measured against. There are two questions with deliberately different
// answers, and every rule for either lives here.
//
// The gate's drop rule asks "does merging this lose coverage?", so it
// always compares against the default branch (gateDropBase) — a branch's
// own history would let a PR ratchet coverage down push by push.
//
// Everything a reader sees as "the change" asks "what moved since the last
// good build here?": the branch's own previous gate-passing coverage,
// falling back to the default branch for a branch with none yet — among
// what came before it, never after (deltaBase, CommitBaseline,
// UploadBaseline) — or, on the dashboard's sparklines, a branch's previous
// passing report within the history at hand (ReportBaseline). Gate-failing rows never serve as a baseline, so
// re-running CI cannot launder a failure into the comparison, and neither
// do PR builds: a feature branch's history includes its PR's builds, but
// what the branch is measured against is always a build of the branch
// itself.
//
// A commit's report is merged from parts that arrive one by one, so either
// side of a comparison may be missing some. The commit-report baselines
// compare like with like: a report missing any of the commit's parts — one
// still in flight, or a part that did not run — is passed over, and when
// the commit has fewer parts than its baseline, the baseline is counted
// over the commit's parts alone (comparableBase).

package core

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// passedReports is what the commit-report baselines read; the store
// answers it before an upload is stored, a commit-report transaction while
// the merge runs.
type passedReports interface {
	LatestPassedCommitReport(ctx context.Context, repoID int64, branch string, beforeID int64, excludeCommit string) (*store.CommitReport, error)
	LatestUploadsPerPart(ctx context.Context, repoID int64, commitSHA string) ([]*store.Upload, error)
	partFiles
}

// baselineSearchDepth bounds how many newer reports lacking one of the
// commit's parts a baseline search passes over before giving up — a part
// in flight leaves one such report, a part that stopped running a run of
// them, and the comparison is decoration not worth a long walk.
const baselineSearchDepth = 10

// comparableBase is a baseline report together with its parts' latest
// uploads, so its total can be counted over a subset of them.
type comparableBase struct {
	Report *store.CommitReport
	parts  []*store.Upload
}

// comparableReport returns the newest gate-passing, non-PR report on
// branch that carries every one of parts, skipping the commit's own report
// and, with beforeID > 0, anything not older than that one. A report
// missing one of them would compare a total over different code.
// store.ErrNotFound when there is none within baselineSearchDepth.
func comparableReport(ctx context.Context, reports passedReports, repoID int64, branch, commit string, beforeID int64, parts []string) (*comparableBase, error) {
	for range baselineSearchDepth {
		cr, err := reports.LatestPassedCommitReport(ctx, repoID, branch, beforeID, commit)
		if err != nil {
			return nil, err
		}
		ups, err := reports.LatestUploadsPerPart(ctx, repoID, cr.CommitSHA)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(parts, func(name string) bool {
			return !slices.ContainsFunc(ups, func(u *store.Upload) bool { return u.Part == name })
		}) {
			return &comparableBase{Report: cr, parts: ups}, nil
		}
		beforeID = cr.ID
	}
	return nil, store.ErrNotFound
}

// totalOver is the base's coverage counted over the named parts only —
// its merged total when it has no others, so the common case reads
// nothing more. The parts are a subset of the base's (comparableReport).
func (b *comparableBase) totalOver(ctx context.Context, files partFiles, parts []string) (float64, error) {
	if len(b.parts) == len(parts) {
		return b.Report.TotalPct, nil
	}
	sub := slices.DeleteFunc(slices.Clone(b.parts), func(u *store.Upload) bool { return !slices.Contains(parts, u.Part) })
	covered, total, err := mergedCoverage(ctx, files, sub)
	if err != nil {
		return 0, err
	}
	return profile.Percent(covered, total), nil
}

// gateDropBase returns the total of the gate's drop baseline, or nil when
// the drop rule is off or has nothing to compare against: the default
// branch's latest passing merged report carrying every one of parts,
// counted over those parts. The commit's own report is skipped so an
// earlier part is never its own baseline. The result is recorded with the
// row (GateBasePct) so the verdict can later be explained against the
// comparison the gate actually made.
func gateDropBase(ctx context.Context, reports passedReports, repo *store.Repo, commit string, parts []string) (*float64, error) {
	if repo.Gate.MaxCoverageDrop == nil {
		return nil, nil
	}
	base, err := comparableReport(ctx, reports, repo.ID, repo.DefaultBranch, commit, 0, parts)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading gate baseline: %w", err)
	}
	pct, err := base.totalOver(ctx, reports, parts)
	if err != nil {
		return nil, fmt.Errorf("loading gate baseline: %w", err)
	}
	return new(pct), nil
}

// deltaBase returns the merged report a commit's delta is measured
// against: the previous gate-passing report on its branch carrying every
// one of the commit's parts, falling back to the default branch for a
// first-time feature branch. The commit's own report is skipped so an
// earlier part is never its own baseline, and beforeID > 0 keeps only
// reports older than that one. nil when there is none.
func deltaBase(ctx context.Context, reports passedReports, repo *store.Repo, branch, commit string, beforeID int64, parts []string) (*comparableBase, error) {
	prev, err := comparableReport(ctx, reports, repo.ID, branch, commit, beforeID, parts)
	if errors.Is(err, store.ErrNotFound) && branch != repo.DefaultBranch {
		prev, err = comparableReport(ctx, reports, repo.ID, repo.DefaultBranch, commit, beforeID, parts)
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading baseline report: %w", err)
	}
	return prev, nil
}

// CommitBaseline is deltaBase for a stored report, for the pages that show
// a commit with every part merged: the report it is measured against among
// those older than it, so the comparison stays the one it had when the
// commit arrived. nil when there is none, or the read fails — the
// comparison is decoration, never worth failing a page over.
func CommitBaseline(ctx context.Context, reports passedReports, repo *store.Repo, cr *store.CommitReport) *store.CommitReport {
	ups, err := reports.LatestUploadsPerPart(ctx, repo.ID, cr.CommitSHA)
	if err != nil {
		return nil
	}
	parts := make([]string, len(ups))
	for i, u := range ups {
		parts[i] = u.Part
	}
	base, err := deltaBase(ctx, reports, repo, cr.Branch, cr.CommitSHA, cr.ID, parts)
	if err != nil || base == nil {
		return nil
	}
	return base.Report
}

// ReportBaseline pairs a branch's newest merged report (reports come newest
// first) with the one it is compared against on the branch's trend: the
// most recent gate-passing, non-PR report before it. base is nil when none
// of the given reports qualifies.
func ReportBaseline(reports []*store.CommitReport) (current, base *store.CommitReport) {
	if len(reports) == 0 {
		return nil, nil
	}
	if i := slices.IndexFunc(reports[1:], func(cr *store.CommitReport) bool { return cr.PRID == "" && !cr.GateFailed }); i >= 0 {
		base = reports[1+i]
	}
	return reports[0], base
}

// passedUploads is the one read UploadBaseline needs.
type passedUploads interface {
	LatestPassedUpload(ctx context.Context, repoID int64, branch string, beforeID int64, excludeCommit string) (*store.Upload, error)
}

// UploadBaseline is deltaBase at upload granularity, for the pages that
// compare one upload's files with an earlier one: the newest earlier
// non-PR, gate-passing upload on the same branch, falling back to the
// default branch's newest earlier passing upload for PR and feature-branch
// builds. Both look only at uploads older than u, so a page keeps the
// comparison it had when u arrived rather than drifting to whatever the
// default branch received since — a PR's own merge commit among them.
// nil when there is nothing to compare against, or the read fails — the
// comparison is decoration, never worth failing a page over.
func UploadBaseline(ctx context.Context, uploads passedUploads, repo *store.Repo, u *store.Upload) *store.Upload {
	base, err := uploads.LatestPassedUpload(ctx, repo.ID, u.Branch, u.ID, "")
	if err != nil && u.Branch != repo.DefaultBranch {
		base, err = uploads.LatestPassedUpload(ctx, repo.ID, repo.DefaultBranch, u.ID, u.CommitSHA)
	}
	if err != nil {
		return nil
	}
	return base
}
