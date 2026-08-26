// Accepting one upload: the whole path a parsed coverage profile takes
// once the transport has read and validated it — store the raw profile,
// measure the diff against the PR, evaluate the gate, persist the rows,
// merge the commit's parts and report the result to the forge.

package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// Submission is one upload as the transport parsed it: the fields of the
// request after validation, and the profile they named. Anything that had
// to be read out of an HTTP request — the provenance metadata, the forge
// client the repo's workspace is connected through — is passed in rather
// than discovered here.
type Submission struct {
	Repo       *store.Repo
	Commit     string
	Branch     string
	PRID       string // empty when not a PR build
	Part       string
	Format     string
	PathPrefix string
	Raw        []byte
	Profile    *profile.Profile
	Meta       store.UploadMeta
}

// Result is what the uploader is told: the row that was stored, the merged
// view of the whole commit that everything outward-facing is driven from,
// and what each forge surface did with it.
type Result struct {
	Upload     *store.Upload
	Merged     *Merged
	DiffStatus string
	Push       PushResult
}

// Accept runs a submission through the pipeline. Reaching the forge is
// best effort throughout — an upload lands whether or not the workspace
// has a working connection, and the result says what happened instead.
//
// An error means the upload did not fully land, and the caller should say
// so: the CI client is expected to retry, and a retry is safe because
// every upload recomputes the whole commit.
func (p *Pipeline) Accept(ctx context.Context, sub Submission) (*Result, error) {
	// The client every forge surface below publishes through; nil when the
	// repo's workspace has no connection.
	var fg forge.Forge
	var fgErr error
	if p.Forges != nil {
		fg, fgErr = p.Forges.For(ctx, sub.Repo)
	}

	covered, total := sub.Profile.Coverage()
	totalPct := profile.Percent(covered, total)

	dropDelta, err := p.baselineDelta(ctx, sub.Repo, sub.Commit, totalPct)
	if err != nil {
		return nil, err
	}
	blobKey, err := p.storeRaw(ctx, sub.Repo.ID, sub.Raw)
	if err != nil {
		return nil, fmt.Errorf("storing raw profile: %w", err)
	}

	var diffResult *diffcov.Result
	var diffStatus string
	if sub.PRID != "" {
		diffResult, diffStatus = p.diffCoverage(ctx, fg, fgErr, sub.Repo, sub.PRID, sub.Profile, sub.Format, sub.PathPrefix)
	}

	gate := EvaluateGate(sub.Repo.Gate, totalPct, dropDelta, diffResult)
	upload, files := sub.rows(blobKey, diffResult, gate, covered, total)
	if err := p.Store.CreateUpload(ctx, upload, files); err != nil {
		// The raw profile was already written; don't leave it orphaned.
		if delErr := p.Blobs.Delete(ctx, blobKey); delErr != nil {
			p.Log.Error("cleaning up blob after failed upload", "key", blobKey, "err", delErr)
		}
		return nil, fmt.Errorf("saving upload: %w", err)
	}

	// Recompute the commit's merged report from every part's latest upload
	// and drive all outward-facing surfaces from it, so a commit uploaded
	// in several parts reports its combined total, not the last part in.
	merged, err := p.Recompute(ctx, sub.Repo, upload)
	if err != nil {
		// The upload row is already committed; a recompute failure (including
		// the bounded-timeout case) is still reported as a failure so the CI
		// client sees the upload didn't fully land. It self-heals: the next
		// part's upload — or a retry of this one — recomputes the commit again.
		return nil, fmt.Errorf("computing merged report: %w", err)
	}

	return &Result{
		Upload:     upload,
		Merged:     merged,
		DiffStatus: diffStatus,
		Push:       p.Push(ctx, fg, fgErr, sub.Repo, upload, merged),
	}, nil
}

// sourceExts maps a profile format to the extensions of source files whose
// absence from the coverage report is worth flagging in diff coverage.
var sourceExts = map[string][]string{
	"go":        {".go"},
	"lcov":      {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".vue", ".svelte"},
	"jacoco":    {".java", ".kt", ".kts", ".scala", ".groovy"},
	"cobertura": {".py", ".cs", ".php", ".cpp", ".cc", ".c"},
}

// diffCoverage fetches the PR diff from the forge and intersects it
// with the parsed profile. Best effort: any failure is reported in the
// returned status, never as an upload error.
func (p *Pipeline) diffCoverage(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, prID string, prof *profile.Profile, format, pathPrefix string) (*diffcov.Result, string) {
	if fgErr != nil {
		return nil, "error: " + fgErr.Error()
	}
	if fg == nil {
		return nil, "skipped: no forge connection"
	}
	diffText, err := fg.GetPRDiff(ctx, repo.Slug, prID)
	if errors.Is(err, forge.ErrNotImplemented) {
		return nil, "skipped: diff not supported by forge"
	}
	if err != nil {
		p.Log.Error("fetch PR diff", "repo", repo.Slug, "pr", prID, "err", err)
		return nil, "error: fetching PR diff: " + err.Error()
	}
	added, err := diffcov.ParseUnifiedDiff(strings.NewReader(diffText))
	if err != nil {
		p.Log.Error("parse PR diff", "repo", repo.Slug, "pr", prID, "err", err)
		return nil, "error: parsing PR diff: " + err.Error()
	}

	files := make([]diffcov.FileBlocks, 0, len(prof.Files))
	for _, f := range prof.Files {
		files = append(files, diffcov.FileBlocks{Path: f.Path, Blocks: f.Blocks})
	}
	result := diffcov.Compute(files, added, pathPrefix)

	// Keep only source files in the "changed but no coverage data" list;
	// docs, configs etc. are expected to be absent from the profile.
	if exts := sourceExts[format]; len(exts) > 0 {
		var src []string
		for _, p := range result.UnmatchedFiles {
			for _, ext := range exts {
				if strings.HasSuffix(p, ext) {
					src = append(src, p)
					break
				}
			}
		}
		result.UnmatchedFiles = src
	}
	return result, "computed"
}

func (p *Pipeline) storeRaw(ctx context.Context, repoID int64, raw []byte) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key := fmt.Sprintf("profiles/%d/%s", repoID, hex.EncodeToString(buf))
	if err := p.Blobs.Put(ctx, key, raw); err != nil {
		return "", err
	}
	return key, nil
}

// baselineDelta returns this upload's coverage difference to the gate's
// baseline, or nil when the drop rule is off or has nothing to compare
// against. The rule always compares against the default branch, so a PR
// cannot lower coverage step by step within tolerance.
func (p *Pipeline) baselineDelta(ctx context.Context, repo *store.Repo, commit string, totalPct float64) (*float64, error) {
	if repo.Gate.MaxCoverageDrop == nil {
		return nil, nil
	}
	base, err := p.Store.LatestPassedCommitReport(ctx, repo.ID, repo.DefaultBranch, commit)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("loading gate baseline: %w", err)
	}
	if base == nil {
		return nil, nil
	}
	d := totalPct - base.TotalPct
	return &d, nil
}

// rows turns the submission into the upload row and its per-file rows.
//
// The upload row keeps its own single-part gate result (its gate_failed
// column still feeds the per-upload web views); the response, forge
// status, gate and PR comment are driven by the merged report computed
// after the row is stored.
func (sub Submission) rows(blobKey string, diff *diffcov.Result, gate Verdict, covered, total int64) (*store.Upload, []*store.UploadFile) {
	upload := &store.Upload{
		RepoID:       sub.Repo.ID,
		CommitSHA:    sub.Commit,
		Branch:       sub.Branch,
		PRID:         sub.PRID,
		Format:       sub.Format,
		TotalPct:     profile.Percent(covered, total),
		CoveredStmts: covered,
		TotalStmts:   total,
		RawBlobKey:   blobKey,
		DiffCoverage: diff,
		GateFailed:   gate.Failed(),
		PathPrefix:   sub.PathPrefix,
		Part:         sub.Part,
		Meta:         sub.Meta,
	}
	files := make([]*store.UploadFile, 0, len(sub.Profile.Files))
	for i := range sub.Profile.Files {
		f := &sub.Profile.Files[i]
		c, t := f.Coverage()
		files = append(files, &store.UploadFile{
			Path:         f.Path,
			Pct:          profile.Percent(c, t),
			CoveredStmts: c,
			TotalStmts:   t,
			Blocks:       f.Blocks,
		})
	}
	return upload, files
}
