// What gocov writes back to the forge, and the client it writes with:
// resolving the workspace's one-click connection, then the three
// surfaces an upload can update — the build status that gates a merge,
// the code-insights report, and the PR comment. Everything here is best
// effort: each helper returns a status string for the upload response
// and never fails the upload itself.
package server

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

// forgeFor builds a forge client for the repo through the workspace's
// one-click connection (GitHub App installation, Bitbucket grant or
// GitLab grant). Returns (nil, nil) when the repo's workspace has no
// connection — there is no manual-credential fallback.
func (s *Server) forgeFor(ctx context.Context, repo *store.Repo) (forge.Forge, error) {
	// The workspace is looked up lazily: only when a connection could
	// apply, so a forge that supports no one-click connect skips the
	// query entirely.
	if s.oneClickCapable(repo.Forge) {
		ws := s.repoWorkspace(ctx, repo.Slug, repo.Forge)
		if fg := s.connectedForge(ctx, ws, repo.Forge); fg != nil {
			return fg, nil
		}
	}
	return nil, nil
}

// oneClickCapable reports whether a one-click connection could supply
// credentials for the forge — the gate for the extra workspace lookup.
func (s *Server) oneClickCapable(forgeName string) bool {
	return (s.githubApp != nil && forgeName == "github") ||
		(s.bbConnect != nil && forgeName == "bitbucket") ||
		(s.glConnect != nil && forgeName == "gitlab")
}

// connectedForge returns the workspace's one-click-connected client —
// GitHub App installation, Bitbucket grant or GitLab grant — or nil,
// the top link of the credential chain (D4/D7).
func (s *Server) connectedForge(ctx context.Context, ws *store.Workspace, forgeName string) forge.Forge {
	if fg := s.installationForge(ctx, ws, forgeName); fg != nil {
		return fg
	}
	if fg := s.grantForge(ctx, ws, forgeName); fg != nil {
		return fg
	}
	return s.gitlabGrantForge(ctx, ws, forgeName)
}

// repoWorkspace returns the workspace owning the slug's prefix, nil when
// there is none. Prefixes are tried longest first, so a repo below a
// registered GitLab subgroup resolves to that subgroup's workspace, not a
// same-named ancestor. A lookup failure only degrades down the credential
// chain — forge surfaces are best-effort everywhere else too. The forge
// must match: prefixes are globally unique, and a same-named workspace
// on another forge must not lend its secrets or its installation.
func (s *Server) repoWorkspace(ctx context.Context, slug, forgeName string) *store.Workspace {
	for _, prefix := range slugPrefixes(slug) {
		ws, err := s.store.WorkspaceByPrefix(ctx, prefix)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			s.log.Error("workspace lookup", "repo", slug, "err", err)
			return nil
		}
		if ws.Forge != forgeName {
			return nil
		}
		return ws
	}
	return nil
}

// pushBuildStatus posts a "coverage: X% (±Y)" build status to the repo's
// forge; a failed coverage gate turns the state into FAILED so the forge
// can block the merge. Best effort: push failures are reported in the
// response but do not fail the upload.
func (s *Server) pushBuildStatus(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, u *store.Upload, deltaPct *float64, gate gateResult) string {
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
	if gate.failed() {
		state = forge.StateFailed
		// Forge description fields are short; one reason has to do.
		desc += " — " + gate.failures[0]
	}
	status := forge.BuildStatus{
		Key:         "gocov/coverage",
		State:       state,
		Name:        "gocov",
		Description: desc,
		URL:         s.uploadURL(u),
	}
	if err := fg.PostBuildStatus(ctx, repo.Slug, u.CommitSHA, status); err != nil {
		s.log.Error("post build status", "repo", repo.Slug, "commit", u.CommitSHA, "err", err)
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
func (s *Server) pushCodeInsights(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, u *store.Upload, deltaPct *float64, gate gateResult) string {
	if fgErr != nil {
		return "error: " + fgErr.Error()
	}
	if fg == nil {
		s.log.Debug("code insights skipped: no forge connection", "repo", repo.Slug)
		return "skipped"
	}
	report, annotations := s.insightsReport(u, deltaPct, gate)
	err := fg.PublishReport(ctx, repo.Slug, u.CommitSHA, report, annotations)
	if errors.Is(err, forge.ErrNotImplemented) {
		// A wrapped sentinel carries the forge's reason (e.g. GitHub
		// check runs being closed to the credential type) — worth
		// surfacing, unlike the bare "this forge has no such surface".
		if err == forge.ErrNotImplemented {
			return "skipped"
		}
		s.log.Info("code insights unavailable", "repo", repo.Slug, "reason", err)
		return "skipped: " + err.Error()
	}
	if err != nil {
		s.log.Warn("publish code insights report", "repo", repo.Slug, "commit", u.CommitSHA, "err", err)
		return "error: " + err.Error()
	}
	return "posted"
}

// insightsReport builds the report card and its annotations. The data
// fields stay well under the forge API's cap of ten; annotations exist
// only for PR uploads, and only on uncovered changed lines.
func (s *Server) insightsReport(u *store.Upload, deltaPct *float64, gate gateResult) (forge.Report, []forge.Annotation) {
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
	if gate.configured {
		data = append(data, forge.ReportData{Title: "Gate", Type: forge.DataText, Value: gate.String()})
		if gate.failed() {
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
		Link:    s.uploadURL(u),
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

// prCommentMarker identifies gocov's own comment on a PR; every body
// built by prCommentBody starts with it, so repeated uploads update the
// existing comment instead of stacking new ones.
const prCommentMarker = "**gocov**"

// pushPRComment posts or updates the coverage summary comment on the pull
// request. Returns "" for non-PR uploads so the field is omitted from the
// response.
func (s *Server) pushPRComment(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, u *store.Upload, deltaPct *float64, gate gateResult) string {
	if u.PRID == "" {
		return ""
	}
	if fgErr != nil {
		return "error: " + fgErr.Error()
	}
	if fg == nil {
		return "skipped"
	}
	body := s.prCommentBody(u, deltaPct, gate)

	// Best effort update-in-place: any failure falls back to posting a
	// fresh comment, which is never worse than the old behavior.
	commentID, err := fg.FindPRComment(ctx, repo.Slug, u.PRID, prCommentMarker)
	if err != nil && !errors.Is(err, forge.ErrNotImplemented) {
		s.log.Warn("find PR comment", "repo", repo.Slug, "pr", u.PRID, "err", err)
	}
	if commentID != "" {
		if err := fg.UpdatePRComment(ctx, repo.Slug, u.PRID, commentID, body); err == nil {
			return "updated"
		} else {
			s.log.Warn("update PR comment", "repo", repo.Slug, "pr", u.PRID, "comment", commentID, "err", err)
		}
	}

	if err := fg.PostPRComment(ctx, repo.Slug, u.PRID, body); err != nil {
		s.log.Error("post PR comment", "repo", repo.Slug, "pr", u.PRID, "err", err)
		return "error: " + err.Error()
	}
	return "posted"
}

// prCommentMaxFiles caps the uncovered-lines table in PR comments.
const prCommentMaxFiles = 20

func (s *Server) prCommentBody(u *store.Upload, deltaPct *float64, gate gateResult) string {
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
	if gate.configured {
		if gate.failed() {
			fmt.Fprintf(&sb, "- Gate: ❌ %s\n", strings.Join(gate.failures, "; "))
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

	fmt.Fprintf(&sb, "\n[Full report](%s)\n", s.uploadURL(u))
	return sb.String()
}

func (s *Server) uploadURL(u *store.Upload) string {
	return fmt.Sprintf("%s/uploads/%d", strings.TrimSuffix(s.baseURL, "/"), u.ID)
}

// mdPath neutralizes characters that would break the markdown table or the
// surrounding code span in PR comments. Paths come from the PR diff.
var mdPathReplacer = strings.NewReplacer("`", "'", "|", "\\|", "\n", " ", "\r", " ")

func mdPath(p string) string {
	return mdPathReplacer.Replace(p)
}

// pushToForge updates every forge surface for the commit and records the
// outcome of each one in the response.
//
// The pushes run after the locked recompute. Two parts of one commit can
// push concurrently, and forge latency could let an older push land last
// and pin the commit to a stale status. Serialize the push per commit and
// gate it on this upload's version (its id, which rises with the
// most-complete merged state): TryPushStatus runs the push only if it is
// not older than the last successful one, and records the version only
// after the push succeeds — so a failed push doesn't burn the version and
// a later part retries.
func (s *Server) pushToForge(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, upload *store.Upload, rc *mergedRecompute, resp *uploadResponse) {
	merged, mergedDelta, mergedGate := rc.upload, rc.delta, rc.gate

	pushCtx, cancel := context.WithTimeout(ctx, statusPushTimeout)
	defer cancel()
	pushed, err := s.store.TryPushStatus(pushCtx, repo.ID, upload.CommitSHA, upload.ID, func(ctx context.Context) error {
		resp.BuildStatus = s.pushBuildStatus(ctx, fg, fgErr, repo, merged, mergedDelta, mergedGate)
		resp.CodeInsights = s.pushCodeInsights(ctx, fg, fgErr, repo, merged, mergedDelta, mergedGate)
		resp.PRComment = s.pushPRComment(ctx, fg, fgErr, repo, merged, mergedDelta, mergedGate)
		// The build status gates merges; if it didn't post, signal failure so
		// the version isn't advanced and a later part retries. Insights and
		// PR comment are best effort and don't hold back the version.
		if strings.HasPrefix(resp.BuildStatus, "error") {
			return errStatusPushFailed
		}
		return nil
	})
	switch {
	case errors.Is(err, errStatusPushFailed):
		// resp fields already carry the per-surface outcome; nothing to do.
	case err != nil:
		// Lock/tx failure: the push may not have run. Report it rather than
		// leaving the fields blank.
		s.log.Warn("status push lock", "repo", repo.Slug, "commit", upload.CommitSHA, "err", err)
		if resp.BuildStatus == "" {
			resp.BuildStatus = "error: " + err.Error()
			resp.CodeInsights = "error: " + err.Error()
		}
	case !pushed:
		resp.BuildStatus = "skipped: superseded"
		resp.CodeInsights = "skipped: superseded"
		if merged.PRID != "" {
			resp.PRComment = "skipped: superseded"
		}
	}
}
