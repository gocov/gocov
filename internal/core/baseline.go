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
// falling back to the default branch for a branch with none yet
// (deltaBase, UploadBaseline), or on a branch's trend its previous passing
// report (ReportBaseline). Gate-failing rows never serve as a baseline, so
// re-running CI cannot launder a failure into the comparison.

package core

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/gocov/gocov/internal/store"
)

// passedReports is the one read the commit-report baselines need; the
// store answers it before an upload is stored, a commit-report transaction
// while the merge runs.
type passedReports interface {
	LatestPassedCommitReport(ctx context.Context, repoID int64, branch, excludeCommit string) (*store.CommitReport, error)
}

// gateDropBase returns the total of the gate's drop baseline, or nil when
// the drop rule is off or has nothing to compare against: the default
// branch's latest passing merged report. The commit's own report is
// skipped so an earlier part is never its own baseline. The result is
// recorded with the row (GateBasePct) so the verdict can later be
// explained against the comparison the gate actually made.
func gateDropBase(ctx context.Context, reports passedReports, repo *store.Repo, commit string) (*float64, error) {
	if repo.Gate.MaxCoverageDrop == nil {
		return nil, nil
	}
	base, err := reports.LatestPassedCommitReport(ctx, repo.ID, repo.DefaultBranch, commit)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("loading gate baseline: %w", err)
	}
	if base == nil {
		return nil, nil
	}
	return new(base.TotalPct), nil
}

// deltaBase returns the merged report a commit's delta is measured
// against: the previous gate-passing report on its branch, falling back to
// the default branch for a first-time feature branch. The commit's own
// report is skipped so an earlier part is never its own baseline. nil when
// there is none.
func deltaBase(ctx context.Context, reports passedReports, repo *store.Repo, branch, commit string) (*store.CommitReport, error) {
	prev, err := reports.LatestPassedCommitReport(ctx, repo.ID, branch, commit)
	if errors.Is(err, store.ErrNotFound) && branch != repo.DefaultBranch {
		prev, err = reports.LatestPassedCommitReport(ctx, repo.ID, repo.DefaultBranch, commit)
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("loading baseline report: %w", err)
	}
	return prev, nil
}

// ReportBaseline pairs a branch's newest merged report (reports come newest
// first) with the one it is compared against on the branch's trend: the
// most recent gate-passing report before it. base is nil when none of the
// given reports qualifies.
func ReportBaseline(reports []*store.CommitReport) (current, base *store.CommitReport) {
	if len(reports) == 0 {
		return nil, nil
	}
	if i := slices.IndexFunc(reports[1:], func(cr *store.CommitReport) bool { return !cr.GateFailed }); i >= 0 {
		base = reports[1+i]
	}
	return reports[0], base
}

// branchUploads is the one read UploadBaseline needs.
type branchUploads interface {
	ListBranchUploads(ctx context.Context, repoID int64, branch string, limit int) ([]*store.Upload, error)
}

// uploadBaselineScan bounds how far back UploadBaseline reads uploads.
const uploadBaselineScan = 60

// UploadBaseline is deltaBase at upload granularity, for the pages that
// compare one upload's files with an earlier one: the newest earlier
// non-PR, gate-passing upload on the same branch, falling back to the
// default branch's latest passing upload for PR and feature-branch builds.
// nil when there is nothing to compare against, or the read fails — the
// comparison is decoration, never worth failing a page over.
func UploadBaseline(ctx context.Context, uploads branchUploads, repo *store.Repo, u *store.Upload) *store.Upload {
	pick := func(branch string, ok func(*store.Upload) bool) *store.Upload {
		ups, err := uploads.ListBranchUploads(ctx, repo.ID, branch, uploadBaselineScan)
		if err != nil {
			return nil
		}
		if i := slices.IndexFunc(ups, ok); i >= 0 {
			return ups[i]
		}
		return nil
	}
	base := pick(u.Branch, func(prev *store.Upload) bool {
		return prev.ID < u.ID && prev.PRID == "" && !prev.GateFailed
	})
	if base == nil && u.Branch != repo.DefaultBranch {
		base = pick(repo.DefaultBranch, func(prev *store.Upload) bool {
			return prev.CommitSHA != u.CommitSHA && prev.PRID == "" && !prev.GateFailed
		})
	}
	return base
}
