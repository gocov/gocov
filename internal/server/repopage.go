// The repo page (GET /repos/{slug...}): one repo's current verdict, its
// coverage trend, the files behind its latest upload, and a paged list of
// the uploads behind it.

package server

import (
	"cmp"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/store"
)

// handleRepo implements GET /repos/{forge}/{workspace}/{repo}. The page
// answer is the access decision plus the head tags a crawler reads; the
// numbers behind it come from the UI API's twin of this route, so the
// page itself loads only the repo row the decision needs.
func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) {
	repo, _, ok := s.reportRepo(w, r)
	if !ok {
		return
	}
	// Only past the access decision may the response name the repo.
	s.serveApp(w, r, http.StatusOK, s.repoPageHead(repo))
}

// buildRepoPage assembles the repo page for the app: the standing of the
// selected branch, the trend behind it, the files of its latest report and
// a page of uploads. A false second result means the answer — a not-found,
// a refusal or an internal error — is already written.
func (s *Server) buildRepoPage(w http.ResponseWriter, r *http.Request) (*repoPageDTO, bool) {
	repo, member, ok := s.reportRepo(w, r)
	if !ok {
		return nil, false
	}

	branch := r.FormValue("branch")
	page, _ := strconv.Atoi(r.FormValue("page"))
	if page < 0 {
		page = 0
	}

	// The page's reads are independent of each other, so they run side by
	// side: the recent uploads (the branch selector), the page of history,
	// the trend with the files view hanging off its latest report, and the
	// settings button's workspace lookup.
	limit := (page+1)*uploadsPageSize + 1
	// The trend follows the page's branch filter, defaulting to the
	// repo's default branch when "All branches" is selected.
	trendBranch := cmp.Or(branch, repo.DefaultBranch)
	var (
		wg                    sync.WaitGroup
		recent, fetched       []*store.Upload
		recentErr, fetchedErr error
		trendReports          []*store.CommitReport
		trendErr              error
		latest, base          *store.CommitReport
		lastUpload            *store.Upload
		files                 *filesViewDTO
		canSettings           bool
	)
	// Fetch one page beyond the current one so "Older" knows whether to
	// render; the recent list also feeds the branch selector.
	wg.Go(func() {
		recent, recentErr = s.store.ListUploads(r.Context(), repo.ID, recentUploads)
	})
	switch {
	case branch != "":
		wg.Go(func() {
			fetched, fetchedErr = s.store.ListBranchUploads(r.Context(), repo.ID, branch, limit)
		})
	case limit > recentUploads:
		// The recent fetch doubles as the history only while it also
		// covers the sentinel row; at limit == recentUploads+1 it is one
		// row short of deciding "Older" and would hide the link with pages
		// still to come.
		wg.Go(func() {
			fetched, fetchedErr = s.store.ListUploads(r.Context(), repo.ID, limit)
		})
	}
	wg.Go(func() {
		trendReports, trendErr = s.store.ListBranchCommitReports(r.Context(), repo.ID, trendBranch, trendReportLimit)
		if trendErr != nil {
			return
		}
		// The verdict, stats and files view all describe the selected
		// branch's current standing (the default branch when "All
		// branches" is chosen); they ride inside the branch-filtered region
		// so the selector moves them together with the trend and history.
		// trendReports come newest first, so they carry the latest report
		// and, within the last 50, the baseline it is measured against.
		latest, base = core.ReportBaseline(trendReports[:min(baselineLookback, len(trendReports))])
		if latest == nil {
			return
		}
		lu, err := s.store.Upload(r.Context(), latest.UploadID)
		if err != nil {
			return
		}
		lastUpload = lu
		baseUpload, baseFiles := s.baselineUpload(r.Context(), repo, lu)
		if files, err = s.buildFilesView(r.Context(), lu, baseUpload, baseFiles); err != nil {
			s.log.Warn("loading files for repo page", "upload", lu.ID, "err", err)
		}
	})
	// The settings link is for members of a tracked workspace; anyone
	// admitted through the public branch — anonymous or a signed-in
	// non-member — gets neither the button nor the workspace lookup behind
	// it. With sign-in on, membership already is a tracked workspace owning
	// the repo, so only an open instance has one to look up.
	switch {
	case !member:
	case s.authEnabled():
		canSettings = true
	default:
		wg.Go(func() {
			canSettings = s.forges.WorkspaceFor(r.Context(), repo.Slug, repo.Forge) != nil
		})
	}
	wg.Wait()

	if recentErr != nil {
		s.internalError(w, "listing uploads", recentErr)
		return nil, false
	}
	if fetchedErr != nil {
		s.internalError(w, "listing uploads", fetchedErr)
		return nil, false
	}
	if trendErr != nil {
		s.internalError(w, "listing reports for trend", trendErr)
		return nil, false
	}
	if branch == "" && limit <= recentUploads {
		fetched = recent
	}
	seen := map[string]bool{}
	for _, u := range recent {
		seen[u.Branch] = true
	}
	branches := slices.Sorted(maps.Keys(seen))

	start := min(page*uploadsPageSize, len(fetched))
	end := min(start+uploadsPageSize, len(fetched))
	uploads := fetched[start:end]
	hasOlder := len(fetched) > (page+1)*uploadsPageSize

	dto := &repoPageDTO{
		Repo: repoHeadDTO{
			repoRefDTO:    newRepoRefDTO(repo),
			DefaultBranch: repo.DefaultBranch,
			Gate:          newGateDTO(repo.Gate),
			CanSettings:   canSettings,
		},
		Branches:    branches,
		Branch:      branch,
		TrendBranch: trendBranch,
		Trend:       []trendPointDTO{},
		Uploads:     make([]uploadRowDTO, 0, len(uploads)),
		Page:        page,
		HasOlder:    hasOlder,
	}
	if dto.Branches == nil {
		dto.Branches = []string{}
	}
	// The trend reads oldest first and skips PR reports, the same series
	// the chart plots.
	for _, report := range slices.Backward(trendReports) {
		if report.PRID != "" {
			continue
		}
		dto.Trend = append(dto.Trend, trendPointDTO{
			UploadID:   report.UploadID,
			SHA:        report.CommitSHA,
			Coverage:   report.TotalPct,
			At:         report.CreatedAt,
			GateFailed: report.GateFailed,
		})
	}
	for _, u := range uploads {
		dto.Uploads = append(dto.Uploads, uploadRowDTO{
			ID:       u.ID,
			SHA:      u.CommitSHA,
			Branch:   u.Branch,
			PRID:     u.PRID,
			Coverage: u.TotalPct,
			Gate:     gateState(store.JudgedGate(u.Gate, repo.Gate), u.GateFailed),
			At:       u.CreatedAt,
		})
	}
	if latest == nil {
		return dto, true
	}

	summary := &repoSummaryDTO{
		Verdict: gateVerdict("The latest commit", latest.TotalPct, latest.DiffCoverage, latest.GateFailed, store.JudgedGate(latest.Gate, repo.Gate), latest.GateBasePct),
		Commit: repoCommitDTO{
			UploadID:  latest.UploadID,
			SHA:       latest.CommitSHA,
			At:        latest.CreatedAt,
			Branch:    latest.Branch,
			PRID:      latest.PRID,
			IsDefault: latest.Branch == repo.DefaultBranch,
		},
		CoveredStmts: latest.CoveredStmts,
		TotalStmts:   latest.TotalStmts,
	}
	if base != nil {
		summary.Verdict.against(base.UploadID, base.CommitSHA, base.TotalPct)
	}
	if lastUpload != nil {
		summary.LastUpload = &lastUploadDTO{At: lastUpload.CreatedAt, CILabel: ciLabels[lastUpload.Meta.CIProvider]}
		dto.Files = files
	}
	dto.Summary = summary
	return dto, true
}

// repoPageDTO is the repo page for the app. The trend arrives as raw
// points and the files as a flat list: the chart's geometry and the
// directory tree are the client's to build.
type repoPageDTO struct {
	Repo        repoHeadDTO     `json:"repo"`
	Branches    []string        `json:"branches"`
	Branch      string          `json:"branch"`
	TrendBranch string          `json:"trend_branch"`
	Summary     *repoSummaryDTO `json:"summary"`
	Trend       []trendPointDTO `json:"trend"`
	Files       *filesViewDTO   `json:"files"`
	Uploads     []uploadRowDTO  `json:"uploads"`
	Page        int             `json:"page"`
	HasOlder    bool            `json:"has_older"`
}

type repoHeadDTO struct {
	repoRefDTO
	DefaultBranch string  `json:"default_branch"`
	Gate          gateDTO `json:"gate"`
	// CanSettings is the settings button: members of a tracked workspace.
	CanSettings bool `json:"can_settings"`
}

// repoSummaryDTO is the branch's current standing; nil until the branch
// has a report.
type repoSummaryDTO struct {
	Verdict      verdictDTO     `json:"verdict"`
	Commit       repoCommitDTO  `json:"commit"`
	CoveredStmts int64          `json:"covered_stmts"`
	TotalStmts   int64          `json:"total_stmts"`
	LastUpload   *lastUploadDTO `json:"last_upload"`
}

type repoCommitDTO struct {
	UploadID  int64     `json:"upload_id"`
	SHA       string    `json:"sha"`
	At        time.Time `json:"at"`
	Branch    string    `json:"branch"`
	PRID      string    `json:"pr_id"`
	IsDefault bool      `json:"is_default"`
}

type lastUploadDTO struct {
	At      time.Time `json:"at"`
	CILabel string    `json:"ci_label"`
}

// trendPointDTO is one point of the coverage trend, oldest first.
type trendPointDTO struct {
	UploadID   int64     `json:"upload_id"`
	SHA        string    `json:"sha"`
	Coverage   float64   `json:"coverage"`
	At         time.Time `json:"at"`
	GateFailed bool      `json:"gate_failed"`
}

// uploadRowDTO is one row of the upload history.
type uploadRowDTO struct {
	ID       int64     `json:"id"`
	SHA      string    `json:"sha"`
	Branch   string    `json:"branch"`
	PRID     string    `json:"pr_id"`
	Coverage float64   `json:"coverage"`
	Gate     string    `json:"gate"` // pass / fail / none, against the gate it was judged by
	At       time.Time `json:"at"`
}

// handleAPIRepo implements GET /api/ui/repos/{forge}/{slug...}, the repo
// page's data. Like the page it may be read anonymously on a public repo.
func (s *Server) handleAPIRepo(w http.ResponseWriter, r *http.Request) {
	if dto, ok := s.buildRepoPage(w, r); ok {
		s.writeJSON(w, dto)
	}
}

const (
	uploadsPageSize = 10
	// recentUploads bounds the newest-uploads fetch that fills the branch
	// selector, and doubles as the first pages' history without a second query.
	recentUploads = 100
	// trendReportLimit bounds the branch history behind the coverage trend
	// and the dashboard's sparklines.
	trendReportLimit = 60
	// baselineLookback bounds the search for a comparison baseline; a
	// branch whose last 50 reports all failed shows no delta.
	baselineLookback = 50
)
