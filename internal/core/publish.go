// What gocov writes back to the forge: the build status that gates a
// merge, the code-insights report, and the PR comment. Every push is best
// effort — each helper returns the words the uploader sees rather than an
// error that could fail the upload.

package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/store"
)

// statusPushTimeout bounds one round of the forge push. No database
// connection is held across it, but the commit's push lease is, and every
// upload of the commit queues behind it.
const statusPushTimeout = 20 * time.Second

// pushLease is how long a claim on a commit's push stays good: past one
// round's timeout, so a live push never loses it, and short, so a push
// whose task died blocks the commit for no longer than this.
const pushLease = 30 * time.Second

// maxPushRounds bounds how often one upload pushes a commit again for
// uploads that queued behind it. Every round carries all of the commit's
// parts so far, so a burst of queued uploads costs one round, not one each.
const maxPushRounds = 5

// finishPushTimeout bounds releasing the lease. It runs even after the
// request is gone, so a disconnected client does not leave the commit
// blocked for the lease's full length.
const finishPushTimeout = 5 * time.Second

// Queued is what an upload reports for every surface when another upload
// of the same commit was pushing: that push goes round again with the
// commit's newest state, which includes this upload.
const Queued = "queued: pushed with a concurrent upload of this commit"

// pushBuildStatus posts a "coverage: X% (±Y)" build status to the repo's
// forge; a failed coverage gate turns the state into FAILED so the forge
// can block the merge. Best effort: push failures are reported in the
// response but do not fail the upload.
func (p *Pipeline) pushBuildStatus(ctx context.Context, fg forge.Forge, repo *store.Repo, u *store.Upload, link string, deltaPct *float64, gate Verdict) string {
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
		URL:         link,
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
func (p *Pipeline) pushCodeInsights(ctx context.Context, fg forge.Forge, repo *store.Repo, u *store.Upload, link string, deltaPct *float64, gate Verdict) string {
	report, annotations := p.insightsReport(u, link, deltaPct, gate)
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
func (p *Pipeline) insightsReport(u *store.Upload, link string, deltaPct *float64, gate Verdict) (forge.Report, []forge.Annotation) {
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
		Link:    link,
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
	files := uncoveredFiles(dc)
	slices.SortFunc(files, func(a, b diffcov.FileCoverage) int {
		pa := float64(a.CoveredLines) * float64(b.TotalLines)
		pb := float64(b.CoveredLines) * float64(a.TotalLines)
		if c := cmp.Compare(pa, pb); c != 0 {
			return c
		}
		return cmp.Compare(a.Path, b.Path)
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

// uncoveredFiles is the changed files with at least one uncovered line,
// in dc's path order — the only ones a reviewer needs to hear about.
func uncoveredFiles(dc *diffcov.Result) []diffcov.FileCoverage {
	var files []diffcov.FileCoverage
	for _, f := range dc.Files {
		if len(f.UncoveredLines) > 0 {
			files = append(files, f)
		}
	}
	return files
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
		for _, sp := range diffcov.LineSpans(f.UncoveredLines) {
			if len(anns) == insightsMaxAnnotations {
				dropped++
				continue
			}
			summary := fmt.Sprintf("Line %d of this change is not covered by tests", sp.Start)
			if sp.End > sp.Start {
				summary = fmt.Sprintf("Lines %d–%d of this change are not covered by tests", sp.Start, sp.End)
			}
			anns = append(anns, forge.Annotation{Path: f.Path, Line: sp.Start, EndLine: sp.End, Summary: summary})
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
func (p *Pipeline) pushPRComment(ctx context.Context, fg forge.Forge, repo *store.Repo, u *store.Upload, link string, deltaPct *float64, gate Verdict) string {
	if u.PRID == "" {
		return ""
	}
	body := p.prCommentBody(u, link, deltaPct, gate)

	// Best effort update-in-place: any failure falls back to posting a
	// fresh comment, which is never worse than the old behavior.
	commentID, err := fg.FindPRComment(ctx, repo.Slug, u.PRID, PRCommentMarker)
	if err != nil && !errors.Is(err, forge.ErrNotImplemented) {
		p.Log.Warn("find PR comment", "repo", repo.Slug, "pr", u.PRID, "err", err)
	}
	if commentID != "" {
		err := fg.UpdatePRComment(ctx, repo.Slug, u.PRID, commentID, body)
		if err == nil {
			return "updated"
		}
		p.Log.Warn("update PR comment", "repo", repo.Slug, "pr", u.PRID, "comment", commentID, "err", err)
	}

	if err := fg.PostPRComment(ctx, repo.Slug, u.PRID, body); err != nil {
		p.Log.Error("post PR comment", "repo", repo.Slug, "pr", u.PRID, "err", err)
		return "error: " + err.Error()
	}
	return "posted"
}

// prCommentMaxFiles caps the uncovered-lines table in PR comments.
const prCommentMaxFiles = 20

func (p *Pipeline) prCommentBody(u *store.Upload, link string, deltaPct *float64, gate Verdict) string {
	var sb strings.Builder
	short := ShortSHA(u.CommitSHA)
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

		if uncovered := uncoveredFiles(dc); len(uncovered) > 0 {
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
			shown := dc.UnmatchedFiles[:min(n, prCommentMaxFiles)]
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

	fmt.Fprintf(&sb, "\n[Full report](%s)\n", link)
	// Hosted only: self-hosted operators do not get marketing appended to
	// their PRs. Appended after the marker-led body, so update-in-place
	// (PRCommentMarker) is untouched.
	if p.Hosted {
		sb.WriteString("\n<sub>Coverage by [gocov](https://gocov.dev?ref=pr-comment) — free for public repos</sub>\n")
	}
	return sb.String()
}

// reportURL is the page every forge surface links to for the merged
// report u carries: the triggering upload's page, and for a commit
// uploaded in more than one part that page with every part merged
// (?parts=merged) — the numbers the surfaces state, where the upload
// alone would show only its own part.
func (p *Pipeline) reportURL(u *store.Upload, parts int) string {
	url := fmt.Sprintf("%s/uploads/%d", strings.TrimSuffix(p.BaseURL, "/"), u.ID)
	if parts > 1 {
		url += "?parts=merged"
	}
	return url
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

	// timings is for the accept log only; it never reaches the uploader.
	timings pushTimings
}

// pushTimings splits a push's time for the accept log: claiming the
// commit's push lease (one statement), then each surface's forge round
// trip. The surfaces run side by side, so a round takes as long as the
// slowest. ran is false when no surface reached the forge — superseded,
// queued, or the claim failed. rounds counts this upload's pushes of the
// commit: more than one when uploads queued behind it.
type pushTimings struct {
	ran                       bool
	claim                     time.Duration
	status, insights, comment time.Duration
	rounds                    int
}

// Push updates every forge surface for the commit and reports what each
// one did.
//
// The pushes run after the locked recompute. Two parts of one commit can
// push concurrently, and forge latency could let an older push land last
// and pin the commit to a stale status, so one push per commit runs at a
// time, gated on this upload's version (its id): a claim on the commit's
// push lease (store.ClaimPush). The lease is taken and released in one
// statement each, so no connection waits on the forge. An upload that
// finds the lease held queues instead of waiting; the holder then goes
// round again with the commit's newest merged state, so the last word on
// the forge is always the newest one. A failed build status records
// nothing, and a later part retries.
func (p *Pipeline) Push(ctx context.Context, fg forge.Forge, repo *store.Repo, upload *store.Upload, rc *Merged) PushResult {
	if fg == nil {
		p.Log.Debug("forge push skipped: no forge connection", "repo", repo.Slug)
		return everySurface("skipped", upload.PRID != "")
	}
	res, again := p.pushRound(ctx, fg, repo, upload, rc)
	rounds := 1
	// Later rounds run on past a client that has gone: the uploads that
	// queued behind this one are waiting on them.
	bg := context.WithoutCancel(ctx)
	for ; again && rounds < maxPushRounds; rounds++ {
		latest, merged, err := p.latestMerged(bg, repo, upload.CommitSHA)
		if err != nil {
			p.Log.Warn("forge push: reloading the commit", "repo", repo.Slug, "commit", upload.CommitSHA, "err", err)
			again = false
			break
		}
		_, again = p.pushRound(bg, fg, repo, latest, merged)
	}
	if again {
		p.Log.Warn("forge push: rounds exhausted; the commit's next upload pushes it",
			"repo", repo.Slug, "commit", upload.CommitSHA, "rounds", rounds)
	}
	res.timings.rounds = rounds
	return res
}

// pushRound claims the commit's push lease at upload's version, pushes
// rc's surfaces if it got it and releases it, reporting whether uploads
// queued behind it meanwhile.
func (p *Pipeline) pushRound(ctx context.Context, fg forge.Forge, repo *store.Repo, upload *store.Upload, rc *Merged) (PushResult, bool) {
	merged := rc.Upload
	start := time.Now()
	claim, lease, err := p.Store.ClaimPush(ctx, repo.ID, upload.CommitSHA, upload.ID, pushLease)
	claimed := time.Since(start)
	var res PushResult
	switch {
	case err != nil:
		p.Log.Warn("forge push claim", "repo", repo.Slug, "commit", upload.CommitSHA, "err", err)
		res = everySurface("error: "+err.Error(), merged.PRID != "")
	case claim == store.PushSuperseded:
		res = everySurface("skipped: superseded", merged.PRID != "")
	case claim == store.PushQueued:
		res = everySurface(Queued, merged.PRID != "")
	default:
		pushCtx, cancel := context.WithTimeout(ctx, statusPushTimeout)
		res = p.pushSurfaces(pushCtx, fg, repo, merged, p.reportURL(merged, len(rc.Parts)), rc.Delta, rc.Verdict)
		cancel()
		// The build status gates merges; only it holds back the version.
		// Insights and the PR comment are best effort.
		pushed := !strings.HasPrefix(res.BuildStatus, "error")
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishPushTimeout)
		again, err := p.Store.FinishPush(fctx, repo.ID, upload.CommitSHA, upload.ID, lease, pushed)
		cancel()
		if err != nil {
			// The lease lapses on its own; only the queued uploads' round is lost.
			p.Log.Warn("forge push release", "repo", repo.Slug, "commit", upload.CommitSHA, "err", err)
		}
		res.timings.claim = claimed
		return res, again
	}
	res.timings.claim = claimed
	return res, false
}

// latestMerged is the commit's newest state for another push round: its
// merged report recomputed for the newest upload, which every round's
// version must be.
func (p *Pipeline) latestMerged(ctx context.Context, repo *store.Repo, commitSHA string) (*store.Upload, *Merged, error) {
	parts, err := p.Store.LatestUploadsPerPart(ctx, repo.ID, commitSHA)
	if err != nil {
		return nil, nil, err
	}
	if len(parts) == 0 {
		return nil, nil, store.ErrNotFound
	}
	newest := parts[0]
	for _, u := range parts[1:] {
		if u.ID > newest.ID {
			newest = u
		}
	}
	merged, err := p.Recompute(ctx, repo, newest)
	if err != nil {
		return nil, nil, err
	}
	return newest, merged, nil
}

// pushSurfaces drives the three surfaces through the repo's forge client.
// They are independent requests (forge clients hold no mutable state) and
// each writes only its own result field, so they run concurrently: the
// push, which holds the commit's push lease, takes as long as the slowest
// surface rather than the sum of all three. Without a client — the
// workspace has no working connection — every surface is skipped.
func (p *Pipeline) pushSurfaces(ctx context.Context, fg forge.Forge, repo *store.Repo, u *store.Upload, link string, deltaPct *float64, gate Verdict) PushResult {
	if fg == nil {
		p.Log.Debug("code insights skipped: no forge connection", "repo", repo.Slug)
		return everySurface("skipped", u.PRID != "")
	}
	res := PushResult{timings: pushTimings{ran: true}}
	var wg sync.WaitGroup
	t := &res.timings
	wg.Go(func() {
		defer timeInto(&t.status)()
		res.BuildStatus = p.pushBuildStatus(ctx, fg, repo, u, link, deltaPct, gate)
	})
	wg.Go(func() {
		defer timeInto(&t.insights)()
		res.CodeInsights = p.pushCodeInsights(ctx, fg, repo, u, link, deltaPct, gate)
	})
	wg.Go(func() {
		defer timeInto(&t.comment)()
		res.PRComment = p.pushPRComment(ctx, fg, repo, u, link, deltaPct, gate)
	})
	wg.Wait()
	return res
}

// timeInto starts timing into d and returns the call that stops it.
func timeInto(d *time.Duration) func() {
	start := time.Now()
	return func() { *d = time.Since(start) }
}

// everySurface reports one outcome on every surface the upload has. The
// PR comment exists only for PR uploads; otherwise its field stays empty
// and is omitted from the response.
func everySurface(outcome string, pr bool) PushResult {
	res := PushResult{BuildStatus: outcome, CodeInsights: outcome}
	if pr {
		res.PRComment = outcome
	}
	return res
}
