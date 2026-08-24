// Merging the parts of one commit. A commit's coverage may arrive as
// several uploads (backend, frontend, e2e, ...), and every upload
// rebuilds the commit's merged report from the latest upload of every
// part — that merged view, not the individual upload, is what the
// response, the gate and every forge surface are driven from.

package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// Recompute rebuilds the merged report for the upload's commit
// from the latest upload of every part, persists it, and returns the merged
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
		parts, err := tx.LatestUploadsPerPart(ctx, repo.ID, u.CommitSHA)
		if err != nil {
			return fmt.Errorf("loading commit parts: %w", err)
		}

		profiles := make([]*profile.Profile, 0, len(parts))
		diffs := make([]*diffcov.Result, 0, len(parts))
		for _, p := range parts {
			files, err := tx.UploadFiles(ctx, p.ID)
			if err != nil {
				return fmt.Errorf("loading part files: %w", err)
			}
			prof := &profile.Profile{Files: make([]profile.File, 0, len(files))}
			for _, f := range files {
				prof.Files = append(prof.Files, profile.File{Path: f.Path, Blocks: f.Blocks})
			}
			profiles = append(profiles, prof)
			if p.DiffCoverage != nil {
				diffs = append(diffs, p.DiffCoverage)
			}
		}

		merged := profile.Merge(profiles...)
		covered, total := merged.Coverage()
		totalPct := profile.Percent(covered, total)
		mergedDiff, diffConflicts := diffcov.Merge(diffs...)
		var warnings []string
		if len(diffConflicts) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"diff coverage merged conservatively for %d changed file(s) whose parts disagree on their changed lines (%s); merged coverage is a safe lower bound",
				len(diffConflicts), strings.Join(diffConflicts, ", ")))
		}

		// Delta vs the previous gate-passing merged report on the branch,
		// falling back to the default branch for first-time feature branches.
		// The commit's own report is skipped so an earlier part is never its
		// own baseline.
		var deltaPct *float64
		prev, err := tx.LatestPassedCommitReport(ctx, repo.ID, u.Branch, u.CommitSHA)
		if errors.Is(err, store.ErrNotFound) && u.Branch != repo.DefaultBranch {
			prev, err = tx.LatestPassedCommitReport(ctx, repo.ID, repo.DefaultBranch, u.CommitSHA)
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("loading baseline report: %w", err)
		}
		if prev != nil {
			d := totalPct - prev.TotalPct
			deltaPct = &d
		}

		// The gate drop rule always compares against the default branch's
		// latest passing merged report, so a PR cannot ratchet coverage down
		// part by part within tolerance.
		var dropDelta *float64
		if repo.Gate.MaxCoverageDrop != nil {
			base, err := tx.LatestPassedCommitReport(ctx, repo.ID, repo.DefaultBranch, u.CommitSHA)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("loading gate baseline report: %w", err)
			}
			if base != nil {
				d := totalPct - base.TotalPct
				dropDelta = &d
			}
		}

		gate := EvaluateGate(repo.Gate, totalPct, dropDelta, mergedDiff)

		cr := &store.CommitReport{
			RepoID:       repo.ID,
			CommitSHA:    u.CommitSHA,
			Branch:       u.Branch,
			PRID:         u.PRID,
			TotalPct:     totalPct,
			CoveredStmts: covered,
			TotalStmts:   total,
			GateFailed:   gate.Failed(),
			DiffCoverage: mergedDiff,
			PartCount:    len(parts),
			UploadID:     u.ID,
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
		result = &Merged{
			Upload:   mergedUpload,
			Delta:    deltaPct,
			Verdict:  gate,
			Warnings: warnings,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
