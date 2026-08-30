// The repo page (GET /repos/{slug...}): one repo's current verdict, its
// coverage trend, the files dragging it down, and a paged list of the
// uploads behind it.

package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/store"
)

// handleRepo implements GET /repos/{workspace}/{repo} — stats, badge embed,
// branch filter and the upload list.
func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	repo, err := s.store.RepoBySlug(r.Context(), slug)
	if errors.Is(err, store.ErrNotFound) {
		s.reportNotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, "loading repo", err)
		return
	}
	member, ok := s.authorizeReport(w, r, repo)
	if !ok {
		return
	}

	branch := r.FormValue("branch")
	page, _ := strconv.Atoi(r.FormValue("page"))
	if page < 0 {
		page = 0
	}

	// Fetch one page beyond the current one so "Older" knows whether to
	// render; the recent list also feeds the branch selector.
	recent, err := s.store.ListUploads(r.Context(), repo.ID, recentUploads)
	if err != nil {
		s.internalError(w, "listing uploads", err)
		return
	}
	var branches []string
	seen := map[string]bool{}
	for _, u := range recent {
		if !seen[u.Branch] {
			seen[u.Branch] = true
			branches = append(branches, u.Branch)
		}
	}
	sort.Strings(branches)

	limit := (page+1)*uploadsPageSize + 1
	var fetched []*store.Upload
	if branch == "" {
		// Reuse the branch-selector fetch only while it also covers the
		// sentinel row; at limit == recentUploads+1 it is one row short of
		// deciding "Older" and would hide the link with pages still to come.
		if limit <= recentUploads {
			fetched = recent
		} else if fetched, err = s.store.ListUploads(r.Context(), repo.ID, limit); err != nil {
			s.internalError(w, "listing uploads", err)
			return
		}
	} else if fetched, err = s.store.ListBranchUploads(r.Context(), repo.ID, branch, limit); err != nil {
		s.internalError(w, "listing branch uploads", err)
		return
	}
	start := min(page*uploadsPageSize, len(fetched))
	end := min(start+uploadsPageSize, len(fetched))
	uploads := fetched[start:end]
	hasOlder := len(fetched) > (page+1)*uploadsPageSize

	// The trend follows the page's branch filter, defaulting to the
	// repo's default branch when "All branches" is selected.
	trendBranch := branch
	if trendBranch == "" {
		trendBranch = repo.DefaultBranch
	}
	trendReports, err := s.store.ListBranchCommitReports(r.Context(), repo.ID, trendBranch, trendReportLimit)
	if err != nil {
		s.internalError(w, "listing reports for trend", err)
		return
	}

	// The verdict, stats and "where coverage is missing" all describe the
	// selected branch's current standing (the default branch when "All
	// branches" is chosen); they ride inside the branch-filtered region so the
	// selector moves them together with the trend and history.
	var latest *store.CommitReport
	if l, err := s.store.LatestCommitReport(r.Context(), repo.ID, trendBranch); err == nil {
		latest = l
	} else if !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, "loading latest report", err)
		return
	}

	var (
		verdict   *repoVerdictView
		lastProv  *provView
		miss      *missView
		uncovered int64
	)
	if latest != nil {
		_, base := s.branchBaseReport(r.Context(), repo.ID, trendBranch)
		verdict = s.repoVerdict(latest, repo, base)
		uncovered = latest.TotalStmts - latest.CoveredStmts
		if lu, err := s.store.Upload(r.Context(), latest.UploadID); err == nil {
			p := s.uploadProvenance(r.Context(), lu)
			lastProv = &p
			miss = s.buildMissView(r.Context(), lu)
		}
	}

	var trend *trendView
	if repo.Gate.MinCoverage != nil {
		trend = newTrendView(trendBranch, trendReports, *repo.Gate.MinCoverage)
	} else {
		trend = newTrendView(trendBranch, trendReports)
	}

	// The settings link is for members; anyone admitted through the
	// public branch — anonymous or a signed-in non-member — gets neither
	// the button nor the workspace lookup behind it.
	wsPrefix := ""
	if member {
		wsPrefix = s.repoWorkspacePrefix(r.Context(), repo)
	}

	s.render(w, r, "repo.html", map[string]any{
		"Repo":          repo,
		"Latest":        latest,
		"Verdict":       verdict,
		"Uncovered":     uncovered,
		"LastUpload":    lastProv,
		"Miss":          miss,
		"WSPrefix":      wsPrefix,
		"PublicView":    s.publicView(r),
		"BadgeMarkdown": s.badgeMarkdown(repo.Slug),
		"GateSummary":   gateSummary(repo.Gate),
		"Branches":      branches,
		"Branch":        branch,
		"TrendBranch":   trendBranch,
		"Trend":         trend,
		"Uploads":       uploads,
		"Page":          page,
		"PrevPage":      page - 1,
		"NextPage":      page + 1,
		"HasOlder":      hasOlder,
		"BaseURL":       strings.TrimSuffix(s.baseURL, "/"),
	})
}

const (
	uploadsPageSize = 10
	// recentUploads bounds the newest-uploads fetch that fills the branch
	// selector, and doubles as the first pages' history without a second query.
	recentUploads = 100
)

// branchDelta compares the newest merged report on a branch against the
// most recent gate-passing report before it — the same baseline rule the
// upload API uses, so the UI never shows a delta measured against a report
// that failed the gate. Lookback is bounded; a branch whose last 50 reports
// all failed shows no delta.
func (s *Server) branchDelta(r *http.Request, repoID int64, branch string) *deltaView {
	current, base := s.branchBaseReport(r.Context(), repoID, branch)
	if current == nil || base == nil {
		return nil
	}
	return newDeltaView(current.TotalPct - base.TotalPct)
}

// branchBaseReport returns a branch's newest merged report and the report it
// should be compared against — the most recent gate-passing report before it,
// within a bounded lookback. base is nil when the branch has no earlier
// passing report (a single report, or a run of failures fills the window).
func (s *Server) branchBaseReport(ctx context.Context, repoID int64, branch string) (current, base *store.CommitReport) {
	reports, err := s.store.ListBranchCommitReports(ctx, repoID, branch, 50)
	if err != nil || len(reports) == 0 {
		return nil, nil
	}
	current = reports[0]
	for _, cr := range reports[1:] {
		if !cr.GateFailed {
			return current, cr
		}
	}
	return current, nil
}

// gateSummary renders the repo's gate rules for the stats card.
func gateSummary(g store.Gate) string {
	var parts []string
	if g.MinCoverage != nil {
		parts = append(parts, fmt.Sprintf("total ≥ %.4g%%", *g.MinCoverage))
	}
	if g.MinDiffCoverage != nil {
		parts = append(parts, fmt.Sprintf("diff ≥ %.4g%%", *g.MinDiffCoverage))
	}
	if g.MaxCoverageDrop != nil {
		parts = append(parts, fmt.Sprintf("drop ≤ %.4g%%", *g.MaxCoverageDrop))
	}
	return strings.Join(parts, ", ")
}

// repoVerdictView is the coverage verdict at the top of the repo page: the
// default branch's current standing against its gate, stated once. It mirrors
// the upload page's verdict but reads a merged commit report.
type repoVerdictView struct {
	State      string // "pass", "fail" or "neutral" (no gate configured)
	Pct        float64
	CovClass   string
	Delta      *deltaView // total coverage vs the branch baseline
	Reason     string     // prose walk-through of the gate rules and their outcome
	CommitID   int64      // upload id of the latest commit, for the detail link
	CommitSHA  string
	CommitAgo  string
	Branch     string
	IsDefault  bool
	PRID       string
	BaseID     int64
	BaseSHA    string
	BasePctStr string
}

// repoVerdict assembles the verdict card from the branch's newest merged
// report and the report it is compared against (nil when there is none).
func (s *Server) repoVerdict(latest *store.CommitReport, repo *store.Repo, base *store.CommitReport) *repoVerdictView {
	v := &repoVerdictView{
		Pct:       latest.TotalPct,
		CovClass:  covClass(latest.TotalPct),
		CommitID:  latest.UploadID,
		CommitSHA: latest.CommitSHA,
		CommitAgo: timeAgo(latest.CreatedAt),
		Branch:    latest.Branch,
		IsDefault: latest.Branch == repo.DefaultBranch,
		PRID:      latest.PRID,
	}
	var baseTotal float64
	if base != nil {
		v.Delta = newDeltaView(latest.TotalPct - base.TotalPct)
		v.BaseID = base.UploadID
		v.BaseSHA = base.CommitSHA
		v.BasePctStr = fmt.Sprintf("%.1f%%", base.TotalPct)
		baseTotal = base.TotalPct
	}
	switch {
	case !repo.Gate.Configured():
		v.State = "neutral"
	case latest.GateFailed:
		v.State = "fail"
	default:
		v.State = "pass"
	}
	v.Reason = core.GateReason(latest.TotalPct, latest.DiffCoverage, repo.Gate, baseTotal, base != nil, "The latest commit")
	return v
}

// missFile is one file in the "where coverage is missing" table.
type missFile struct {
	Dir, Base, Path string
	Pct             float64
	Uncovered       int64 // uncovered statements
	Ranges          string
}

// missView lists the files with the most uncovered statements for a branch's
// latest upload, so a reader sees where to aim next.
type missView struct {
	UploadID   int64
	Branch     string
	Files      []missFile // top offenders, most uncovered first
	Total      int        // files with any uncovered statement
	Projection string     // total coverage if the top files were fully covered
	ProjFiles  int        // how many top files that projection assumes (1 or 2)
}

// missTop caps the rows shown in the table.
const missTop = 6

// buildMissView ranks an upload's files by uncovered statements and projects
// where total coverage would land if the worst were fully covered. Returns nil
// when the upload has no per-file data or nothing is uncovered.
func (s *Server) buildMissView(ctx context.Context, u *store.Upload) *missView {
	files, err := s.store.UploadFiles(ctx, u.ID)
	if err != nil || len(files) == 0 {
		return nil
	}
	type ranked struct {
		f   *store.UploadFile
		unc int64
	}
	var offenders []ranked
	for _, f := range files {
		if unc := f.TotalStmts - f.CoveredStmts; unc > 0 {
			offenders = append(offenders, ranked{f, unc})
		}
	}
	if len(offenders) == 0 {
		return nil
	}
	sort.Slice(offenders, func(i, j int) bool {
		if offenders[i].unc != offenders[j].unc {
			return offenders[i].unc > offenders[j].unc
		}
		return offenders[i].f.Path < offenders[j].f.Path
	})

	mv := &missView{UploadID: u.ID, Branch: u.Branch, Total: len(offenders)}
	for i, o := range offenders {
		if i == missTop {
			break
		}
		dir, base := splitPath(o.f.Path)
		mv.Files = append(mv.Files, missFile{
			Dir: dir, Base: base, Path: o.f.Path,
			Pct: o.f.Pct, Uncovered: o.unc, Ranges: uncoveredRanges(o.f.Blocks),
		})
	}

	// Projection: cover the top one or two offenders completely and see where
	// the total lands. Only shown when it moves the needle.
	if u.TotalStmts > 0 {
		n := 2
		if len(offenders) < 2 {
			n = 1
		}
		var recovered int64
		for i := 0; i < n; i++ {
			recovered += offenders[i].unc
		}
		proj := float64(u.CoveredStmts+recovered) / float64(u.TotalStmts) * 100
		if proj-u.TotalPct >= 0.1 {
			mv.Projection = fmt.Sprintf("%.0f%%", math.Floor(proj))
			mv.ProjFiles = n
		}
	}
	return mv
}

// repoWorkspacePrefix resolves the tracked workspace a repo belongs to — its
// most specific registered slug prefix on the same forge — so the page can
// link to that workspace's settings. Returns "" when none is tracked.
func (s *Server) repoWorkspacePrefix(ctx context.Context, repo *store.Repo) string {
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return ""
	}
	for _, prefix := range core.SlugPrefixes(repo.Slug) { // longest (most specific) first
		for _, ws := range workspaces {
			if ws.Forge == repo.Forge && ws.Prefix == prefix {
				return prefix
			}
		}
	}
	return ""
}
