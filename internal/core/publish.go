// What gocov writes back to the forge: the build status that gates a
// merge, the code-insights report, and the PR comment. Every push is best
// effort — each helper returns the words the uploader sees rather than an
// error that could fail the upload.

package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/store"
)

// statusPushTimeout bounds the serialized forge push: TryPushStatus holds a
// per-commit lock (and its connection) across the forge HTTP calls, so a
// hung forge must not pin them.
const statusPushTimeout = 20 * time.Second

// errStatusPushFailed signals TryPushStatus that the build-status push did
// not post, so the status version must not advance and a later part retries.
// It never surfaces as an upload error — the response carries the per-surface
// outcome.
var errStatusPushFailed = errors.New("build status push failed")

// pushBuildStatus posts a "coverage: X% (±Y)" build status to the repo's
// forge; a failed coverage gate turns the state into FAILED so the forge
// can block the merge. Best effort: push failures are reported in the
// response but do not fail the upload.
func (p *Pipeline) pushBuildStatus(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, u *store.Upload, deltaPct *float64, gate Verdict) string {
	if fgErr != nil {
		return "error: " + fgErr.Error()
	}
	if fg == nil {
		return "skipped"
	}

	desc := fmt.Sprintf("coverage: %.1f%%", u.TotalPct)
	if deltaPct != nil {
		desc += fmt.Sprintf(" (%+.1f%%)", *deltaPct)
	}
	state := forge.StateSuccessful
	if gate.Failed() {
		state = forge.StateFailed
		// Forge description fields are short; one reason has to do.
		desc += " — " + gate.Failures[0]
	}
	status := forge.BuildStatus{
		Key:         "gocov/coverage",
		State:       state,
		Name:        "gocov",
		Description: desc,
		URL:         p.uploadURL(u),
	}
	if err := fg.PostBuildStatus(ctx, repo.Slug, u.CommitSHA, status); err != nil {
		p.Log.Error("post build status", "repo", repo.Slug, "commit", u.CommitSHA, "err", err)
		return "error: " + err.Error()
	}
	return "posted"
}

// insightsMaxAnnotations caps report annotations at the forge API's
// per-request limit, keeping the whole publish to one bulk request.
const insightsMaxAnnotations = 100

// pushCodeInsights attaches a coverage report card to the commit and, for
// PR uploads, annotates uncovered changed lines inline in the diff. Best
// effort like the build status: failures land in the response field and
// the log, never in the upload result.
func (p *Pipeline) pushCodeInsights(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, u *store.Upload, deltaPct *float64, gate Verdict) string {
	if fgErr != nil {
		return "error: " + fgErr.Error()
	}
	if fg == nil {
		p.Log.Debug("code insights skipped: no forge connection", "repo", repo.Slug)
		return "skipped"
	}
	report, annotations := p.insightsReport(u, deltaPct, gate)
	err := fg.PublishReport(ctx, repo.Slug, u.CommitSHA, report, annotations)
	if errors.Is(err, forge.ErrNotImplemented) {
		// A wrapped sentinel carries the forge's reason (e.g. GitHub
		// check runs being closed to the credential type) — worth
		// surfacing, unlike the bare "this forge has no such surface".
		if err == forge.ErrNotImplemented {
			return "skipped"
		}
		p.Log.Info("code insights unavailable", "repo", repo.Slug, "reason", err)
		return "skipped: " + err.Error()
	}
	if err != nil {
		p.Log.Warn("publish code insights report", "repo", repo.Slug, "commit", u.CommitSHA, "err", err)
		return "error: " + err.Error()
	}
	return "posted"
}

// insightsReport builds the report card and its annotations. The data
// fields stay well under the forge API's cap of ten; annotations exist
// only for PR uploads, and only on uncovered changed lines.
func (p *Pipeline) insightsReport(u *store.Upload, deltaPct *float64, gate Verdict) (forge.Report, []forge.Annotation) {
	data := []forge.ReportData{
		{Title: "Total coverage", Type: forge.DataPercentage, Value: u.TotalPct},
	}
	if deltaPct != nil {
		data = append(data, forge.ReportData{
			Title: "Change vs base", Type: forge.DataText, Value: fmt.Sprintf("%+.1f%%", *deltaPct)})
	}

	details := "Test coverage uploaded by gocov."
	var annotations []forge.Annotation
	if dc := u.DiffCoverage; dc != nil {
		if dc.TotalLines == 0 {
			details = "No executable lines were changed."
		} else {
			data = append(data,
				forge.ReportData{Title: "Diff coverage", Type: forge.DataPercentage, Value: dc.Percent()},
				forge.ReportData{Title: "Uncovered changed lines", Type: forge.DataNumber,
					Value: float64(dc.TotalLines - dc.CoveredLines)},
			)
			var dropped int
			annotations, dropped = insightsAnnotations(dc)
			details = fmt.Sprintf("%d of %d changed lines are covered by tests.", dc.CoveredLines, dc.TotalLines)
			if dropped > 0 {
				details += fmt.Sprintf(" +%d more uncovered ranges are not annotated — the PR comment lists them all.", dropped)
			}
		}
	}
	data = append(data, forge.ReportData{
		Title: "Statements", Type: forge.DataText, Value: fmt.Sprintf("%d / %d", u.CoveredStmts, u.TotalStmts)})

	result := ""
	if gate.Configured {
		data = append(data, forge.ReportData{Title: "Gate", Type: forge.DataText, Value: gate.String()})
		if gate.Failed() {
			result = forge.ReportFailed
		} else {
			result = forge.ReportPassed
		}
	}
	if dc := u.DiffCoverage; dc != nil {
		data = appendPerFileData(data, dc)
	}

	return forge.Report{
		Title:   "gocov coverage",
		Details: details,
		Result:  result,
		Link:    p.uploadURL(u),
		Data:    data,
	}, annotations
}

// insightsMaxDataFields is the forge API's cap on report data fields.
const insightsMaxDataFields = 10

// appendPerFileData fills the remaining data-field budget with a per-file
// summary of the worst-covered changed files, lowest diff coverage first.
// Fully covered files say nothing a reviewer needs, so they never claim
// a field.
func appendPerFileData(data []forge.ReportData, dc *diffcov.Result) []forge.ReportData {
	var files []diffcov.FileCoverage
	for _, f := range dc.Files {
		if len(f.UncoveredLines) > 0 {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		pi := float64(files[i].CoveredLines) * float64(files[j].TotalLines)
		pj := float64(files[j].CoveredLines) * float64(files[i].TotalLines)
		if pi != pj {
			return pi < pj
		}
		return files[i].Path < files[j].Path
	})
	for _, f := range files {
		if len(data) >= insightsMaxDataFields {
			break
		}
		data = append(data, forge.ReportData{
			Title: dataFieldPath(f.Path),
			Type:  forge.DataPercentage,
			Value: 100 * float64(f.CoveredLines) / float64(f.TotalLines),
		})
	}
	return data
}

// dataFieldPath keeps report data titles readable for deep paths: long
// ones keep their tail, which carries the file name.
func dataFieldPath(p string) string {
	const max = 60
	r := []rune(p)
	if len(r) <= max {
		return p
	}
	return "…" + string(r[len(r)-max+1:])
}

// insightsAnnotations turns the diff-coverage result into one annotation
// per contiguous uncovered range, anchored at the range start and ordered
// by file path (dc.Files is path-sorted). Ranges beyond the cap are
// counted, not annotated.
func insightsAnnotations(dc *diffcov.Result) (anns []forge.Annotation, dropped int) {
	// Whole-file findings first — a changed source file with no coverage
	// data at all. File-level (no line), so the forge pins them to the
	// file header in the diff. They are few and salient, which is why
	// they get the budget before line ranges.
	for _, p := range dc.UnmatchedFiles {
		if len(anns) == insightsMaxAnnotations {
			dropped++
			continue
		}
		anns = append(anns, forge.Annotation{
			Path:    p,
			Summary: "This changed file has no coverage data — nothing in it appears to be tested",
		})
	}
	for _, f := range dc.Files {
		lines := f.UncoveredLines
		for i := 0; i < len(lines); {
			j := i
			for j+1 < len(lines) && lines[j+1] == lines[j]+1 {
				j++
			}
			if len(anns) == insightsMaxAnnotations {
				dropped++
			} else {
				summary := fmt.Sprintf("Line %d of this change is not covered by tests", lines[i])
				if j > i {
					summary = fmt.Sprintf("Lines %d–%d of this change are not covered by tests", lines[i], lines[j])
				}
				anns = append(anns, forge.Annotation{Path: f.Path, Line: lines[i], EndLine: lines[j], Summary: summary})
			}
			i = j + 1
		}
	}
	return anns, dropped
}

// PRCommentMarker identifies gocov's own comment on a PR; every body
// built by prCommentBody starts with it, so repeated uploads update the
// existing comment instead of stacking new ones.
const PRCommentMarker = "**gocov**"

// pushPRComment posts or updates the coverage summary comment on the pull
// request. Returns "" for non-PR uploads so the field is omitted from the
// response.
func (p *Pipeline) pushPRComment(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, u *store.Upload, deltaPct *float64, gate Verdict) string {
	if u.PRID == "" {
		return ""
	}
	if fgErr != nil {
		return "error: " + fgErr.Error()
	}
	if fg == nil {
		return "skipped"
	}
	body := p.prCommentBody(u, deltaPct, gate)

	// Best effort update-in-place: any failure falls back to posting a
	// fresh comment, which is never worse than the old behavior.
	commentID, err := fg.FindPRComment(ctx, repo.Slug, u.PRID, PRCommentMarker)
	if err != nil && !errors.Is(err, forge.ErrNotImplemented) {
		p.Log.Warn("find PR comment", "repo", repo.Slug, "pr", u.PRID, "err", err)
	}
	if commentID != "" {
		if err := fg.UpdatePRComment(ctx, repo.Slug, u.PRID, commentID, body); err == nil {
			return "updated"
		} else {
			p.Log.Warn("update PR comment", "repo", repo.Slug, "pr", u.PRID, "comment", commentID, "err", err)
		}
	}

	if err := fg.PostPRComment(ctx, repo.Slug, u.PRID, body); err != nil {
		p.Log.Error("post PR comment", "repo", repo.Slug, "pr", u.PRID, "err", err)
		return "error: " + err.Error()
	}
	return "posted"
}

// prCommentMaxFiles caps the uncovered-lines table in PR comments.
const prCommentMaxFiles = 20

func (p *Pipeline) prCommentBody(u *store.Upload, deltaPct *float64, gate Verdict) string {
	var sb strings.Builder
	short := u.CommitSHA
	if len(short) > 12 {
		short = short[:12]
	}
	fmt.Fprintf(&sb, "**gocov** report for `%s`\n\n", short)
	fmt.Fprintf(&sb, "- Total coverage: **%.1f%%**", u.TotalPct)
	if deltaPct != nil {
		fmt.Fprintf(&sb, " (%+.1f%%)", *deltaPct)
	}
	sb.WriteString("\n")
	if gate.Configured {
		if gate.Failed() {
			fmt.Fprintf(&sb, "- Gate: ❌ %s\n", strings.Join(gate.Failures, "; "))
		} else {
			sb.WriteString("- Gate: ✅ passed\n")
		}
	}

	if dc := u.DiffCoverage; dc != nil {
		if dc.TotalLines == 0 {
			sb.WriteString("- Diff coverage: no executable lines changed\n")
		} else {
			fmt.Fprintf(&sb, "- Diff coverage: **%.1f%%** (%d/%d changed lines covered)\n",
				dc.Percent(), dc.CoveredLines, dc.TotalLines)
		}

		var uncovered []diffcov.FileCoverage
		for _, f := range dc.Files {
			if len(f.UncoveredLines) > 0 {
				uncovered = append(uncovered, f)
			}
		}
		if len(uncovered) > 0 {
			sb.WriteString("\nUncovered changed lines:\n\n| File | Lines |\n| --- | --- |\n")
			for i, f := range uncovered {
				if i == prCommentMaxFiles {
					fmt.Fprintf(&sb, "| … | and %d more files |\n", len(uncovered)-prCommentMaxFiles)
					break
				}
				fmt.Fprintf(&sb, "| `%s` | %s |\n", mdPath(f.Path), diffcov.Ranges(f.UncoveredLines))
			}
		}
		if n := len(dc.UnmatchedFiles); n > 0 {
			shown := dc.UnmatchedFiles
			if n > prCommentMaxFiles {
				shown = shown[:prCommentMaxFiles]
			}
			escaped := make([]string, len(shown))
			for i, p := range shown {
				escaped[i] = mdPath(p)
			}
			fmt.Fprintf(&sb, "\nChanged files without coverage data: `%s`",
				strings.Join(escaped, "`, `"))
			if n > prCommentMaxFiles {
				fmt.Fprintf(&sb, " and %d more", n-prCommentMaxFiles)
			}
			sb.WriteString("\n")
		}
	}

	fmt.Fprintf(&sb, "\n[Full report](%s)\n", p.uploadURL(u))
	return sb.String()
}

func (p *Pipeline) uploadURL(u *store.Upload) string {
	return fmt.Sprintf("%s/uploads/%d", strings.TrimSuffix(p.BaseURL, "/"), u.ID)
}

// mdPath neutralizes characters that would break the markdown table or the
// surrounding code span in PR comments. Paths come from the PR diff.
var mdPathReplacer = strings.NewReplacer("`", "'", "|", "\\|", "\n", " ", "\r", " ")

func mdPath(p string) string {
	return mdPathReplacer.Replace(p)
}

// PushResult is what each forge surface did with this upload, in the
// words the uploader sees: "posted", "skipped", "skipped: superseded" or
// an "error: ..." explaining why not. None of them is a failure of the
// upload itself.
type PushResult struct {
	BuildStatus  string
	CodeInsights string
	PRComment    string
}

// Push updates every forge surface for the commit and reports what each
// one did.
//
// The pushes run after the locked recompute. Two parts of one commit can
// push concurrently, and forge latency could let an older push land last
// and pin the commit to a stale status. Serialize the push per commit and
// gate it on this upload's version (its id, which rises with the
// most-complete merged state): TryPushStatus runs the push only if it is
// not older than the last successful one, and records the version only
// after the push succeeds — so a failed push doesn't burn the version and
// a later part retries.
func (p *Pipeline) Push(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, upload *store.Upload, rc *Merged) PushResult {
	merged, mergedDelta, mergedGate := rc.Upload, rc.Delta, rc.Verdict
	var res PushResult

	pushCtx, cancel := context.WithTimeout(ctx, statusPushTimeout)
	defer cancel()
	pushed, err := p.Store.TryPushStatus(pushCtx, repo.ID, upload.CommitSHA, upload.ID, func(ctx context.Context) error {
		res.BuildStatus = p.pushBuildStatus(ctx, fg, fgErr, repo, merged, mergedDelta, mergedGate)
		res.CodeInsights = p.pushCodeInsights(ctx, fg, fgErr, repo, merged, mergedDelta, mergedGate)
		res.PRComment = p.pushPRComment(ctx, fg, fgErr, repo, merged, mergedDelta, mergedGate)
		// The build status gates merges; if it didn't post, signal failure so
		// the version isn't advanced and a later part retries. Insights and
		// PR comment are best effort and don't hold back the version.
		if strings.HasPrefix(res.BuildStatus, "error") {
			return errStatusPushFailed
		}
		return nil
	})
	switch {
	case errors.Is(err, errStatusPushFailed):
		// res already carries the per-surface outcome; nothing to do.
	case err != nil:
		// Lock/tx failure: the push may not have run. Report it rather than
		// leaving the fields blank.
		p.Log.Warn("status push lock", "repo", repo.Slug, "commit", upload.CommitSHA, "err", err)
		if res.BuildStatus == "" {
			res.BuildStatus = "error: " + err.Error()
			res.CodeInsights = "error: " + err.Error()
		}
	case !pushed:
		res.BuildStatus = "skipped: superseded"
		res.CodeInsights = "skipped: superseded"
		if merged.PRID != "" {
			res.PRComment = "skipped: superseded"
		}
	}
	return res
}
