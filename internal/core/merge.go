// Merging the parts of one commit. A commit's coverage may arrive as
// several uploads (backend, frontend, e2e, ...), and every upload
// rebuilds the commit's merged report from the latest upload of every
// part — that merged view, not the individual upload, is what the
// response, the gate and every forge surface are driven from.

package core

import (
	"context"
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

		diffs := make([]*diffcov.Result, 0, len(parts))
		for _, p := range parts {
			if p.DiffCoverage != nil {
				diffs = append(diffs, p.DiffCoverage)
			}
		}
		covered, total, err := mergedCoverage(ctx, tx, parts)
		if err != nil {
			return err
		}
		totalPct := profile.Percent(covered, total)
		mergedDiff, diffConflicts := diffcov.Merge(diffs...)
		var warnings []string
		if len(diffConflicts) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"diff coverage merged conservatively for %d changed file(s) whose parts disagree on their changed lines (%s); merged coverage is a safe lower bound",
				len(diffConflicts), strings.Join(diffConflicts, ", ")))
		}

		var deltaPct *float64
		prev, err := deltaBase(ctx, tx, repo, u.Branch, u.CommitSHA)
		if err != nil {
			return err
		}
		if prev != nil {
			deltaPct = new(totalPct - prev.TotalPct)
		}

		dropBase, err := gateDropBase(ctx, tx, repo, u.CommitSHA)
		if err != nil {
			return err
		}

		gate := EvaluateGate(repo.Gate, totalPct, dropBase, mergedDiff)

		cr := &store.CommitReport{
			RepoID:       repo.ID,
			CommitSHA:    u.CommitSHA,
			Branch:       u.Branch,
			PRID:         u.PRID,
			TotalPct:     totalPct,
			CoveredStmts: covered,
			TotalStmts:   total,
			GateFailed:   gate.Failed(),
			GateBasePct:  dropBase,
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

// mergedCoverage counts the covered and total statements of the parts
// merged together. A lone part is its own merge: every parser keys blocks
// by position and files by path, so merging one profile changes nothing,
// and its upload row already carries the totals — the common single-part
// commit reads no files at all. Several parts are read in one query and
// merged, so a block two parts both report counts once.
func mergedCoverage(ctx context.Context, tx store.CommitTx, parts []*store.Upload) (covered, total int64, err error) {
	if len(parts) == 1 {
		return parts[0].CoveredStmts, parts[0].TotalStmts, nil
	}
	ids := make([]int64, len(parts))
	for i, p := range parts {
		ids[i] = p.ID
	}
	files, err := tx.PartFiles(ctx, ids)
	if err != nil {
		return 0, 0, fmt.Errorf("loading part files: %w", err)
	}
	all := &profile.Profile{Files: make([]profile.File, 0, len(files))}
	for _, f := range files {
		all.Files = append(all.Files, profile.File{Path: f.Path, Blocks: f.Blocks})
	}
	covered, total = profile.Merge(all).Coverage()
	return covered, total, nil
}
