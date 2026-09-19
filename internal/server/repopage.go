// The repo page (GET /repos/{slug...}): one repo's current verdict, its
// coverage trend, the files behind its latest upload, and a paged list of
// the uploads behind it.

package server

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gocov/gocov/internal/store"
)

// handleRepo implements GET /repos/{forge}/{workspace}/{repo}. The page
// answer is the access decision plus the head tags a crawler reads; the
// numbers behind it come from the UI API's twin of this route, so the
// page itself loads only the repo row the decision needs.
func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) {
	repo, err := s.store.RepoBySlug(r.Context(), r.PathValue("forge"), r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) {
		s.reportNotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, "loading repo", err)
		return
	}
	if _, ok := s.authorizeReport(w, r, repo); !ok {
		return
	}
	// Only past the access decision may the response name the repo.
	s.serveApp(w, r, http.StatusOK, s.repoPageHead(repo))
}

// repoPageData is one repo's page as read from the store: the standing of
// the selected branch, the trend behind it, the files of its latest
// report and a page of uploads. The UI API copies it into its DTO.
type repoPageData struct {
	Repo *store.Repo
	// Branch is the ?branch filter, empty for "all branches";
	// TrendBranch is the branch the verdict, trend and files describe.
	Branch      string
	TrendBranch string
	Branches    []string
	Latest      *store.CommitReport
	// Base is the report Latest is measured against, nil when the branch
	// has no earlier passing report.
	Base         *store.CommitReport
	Verdict      *verdictView
	LastUpload   *store.Upload
	LastProv     *provView
	FilesView    *filesViewData
	TrendReports []*store.CommitReport
	Settings     bool
	Uploads      []*store.Upload
	Page         int
	HasOlder     bool
}

// buildRepoPage assembles the repo page. A false second result means the
// answer — a not-found, a refusal or an internal error — is already
// written.
func (s *Server) buildRepoPage(w http.ResponseWriter, r *http.Request) (*repoPageData, bool) {
	repo, err := s.store.RepoBySlug(r.Context(), r.PathValue("forge"), r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) {
		s.reportNotFound(w, r)
		return nil, false
	}
	if err != nil {
		s.internalError(w, "loading repo", err)
		return nil, false
	}
	member, ok := s.authorizeReport(w, r, repo)
	if !ok {
		return nil, false
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
		return nil, false
	}
	seen := map[string]bool{}
	for _, u := range recent {
		seen[u.Branch] = true
	}
	branches := slices.Sorted(maps.Keys(seen))

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
			return nil, false
		}
	} else if fetched, err = s.store.ListBranchUploads(r.Context(), repo.ID, branch, limit); err != nil {
		s.internalError(w, "listing branch uploads", err)
		return nil, false
	}
	start := min(page*uploadsPageSize, len(fetched))
	end := min(start+uploadsPageSize, len(fetched))
	uploads := fetched[start:end]
	hasOlder := len(fetched) > (page+1)*uploadsPageSize

	// The trend follows the page's branch filter, defaulting to the
	// repo's default branch when "All branches" is selected.
	trendBranch := cmp.Or(branch, repo.DefaultBranch)
	trendReports, err := s.store.ListBranchCommitReports(r.Context(), repo.ID, trendBranch, trendReportLimit)
	if err != nil {
		s.internalError(w, "listing reports for trend", err)
		return nil, false
	}

	// The verdict, stats and files view all describe the
	// selected branch's current standing (the default branch when "All
	// branches" is chosen); they ride inside the branch-filtered region so the
	// selector moves them together with the trend and history.
	var latest *store.CommitReport
	if l, err := s.store.LatestCommitReport(r.Context(), repo.ID, trendBranch); err == nil {
		latest = l
	} else if !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, "loading latest report", err)
		return nil, false
	}

	d := &repoPageData{
		Repo:         repo,
		Branch:       branch,
		TrendBranch:  trendBranch,
		Branches:     branches,
		Latest:       latest,
		TrendReports: trendReports,
		Uploads:      uploads,
		Page:         page,
		HasOlder:     hasOlder,
	}
	if latest != nil {
		_, base := s.branchBaseReport(r.Context(), repo.ID, trendBranch)
		d.Base = base
		var baseTotal *float64
		if base != nil {
			baseTotal = &base.TotalPct
		}
		d.Verdict = new(gateVerdict("The latest commit", latest.TotalPct, latest.DiffCoverage, latest.GateFailed, repo.Gate, baseTotal))
		if lu, err := s.store.Upload(r.Context(), latest.UploadID); err == nil {
			p := s.uploadProvenance(r.Context(), lu)
			d.LastUpload, d.LastProv = lu, &p
			baseUpload, baseFiles := s.baselineUpload(r.Context(), repo, lu)
			if fv, err := s.buildFilesViewData(r.Context(), lu, baseUpload, baseFiles); err == nil {
				d.FilesView = &fv
			} else {
				s.log.Warn("loading files for repo page", "upload", lu.ID, "err", err)
			}
		}
	}

	// The settings link is for members of a tracked workspace; anyone
	// admitted through the public branch — anonymous or a signed-in
	// non-member — gets neither the button nor the workspace lookup
	// behind it.
	d.Settings = member && s.forges.WorkspaceFor(r.Context(), repo.Slug, repo.Forge) != nil
	return d, true
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
	ID         int64     `json:"id"`
	SHA        string    `json:"sha"`
	Branch     string    `json:"branch"`
	PRID       string    `json:"pr_id"`
	Coverage   float64   `json:"coverage"`
	GateFailed bool      `json:"gate_failed"`
	At         time.Time `json:"at"`
}

// handleAPIRepo implements GET /api/ui/repos/{forge}/{slug...}, the repo
// page's data. Like the page it may be read anonymously on a public repo.
func (s *Server) handleAPIRepo(w http.ResponseWriter, r *http.Request) {
	d, ok := s.buildRepoPage(w, r)
	if !ok {
		return
	}
	dto := repoPageDTO{
		Repo: repoHeadDTO{
			repoRefDTO:    newRepoRefDTO(d.Repo),
			DefaultBranch: d.Repo.DefaultBranch,
			Gate:          newGateDTO(d.Repo.Gate),
			CanSettings:   d.Settings,
		},
		Branches:    d.Branches,
		Branch:      d.Branch,
		TrendBranch: d.TrendBranch,
		Trend:       []trendPointDTO{},
		Files:       newFilesViewDTO(d.FilesView),
		Uploads:     make([]uploadRowDTO, 0, len(d.Uploads)),
		Page:        d.Page,
		HasOlder:    d.HasOlder,
	}
	if dto.Branches == nil {
		dto.Branches = []string{}
	}
	// The trend reads oldest first and skips PR reports, the same series
	// the chart plots.
	for _, report := range slices.Backward(d.TrendReports) {
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
	for _, u := range d.Uploads {
		dto.Uploads = append(dto.Uploads, uploadRowDTO{
			ID:         u.ID,
			SHA:        u.CommitSHA,
			Branch:     u.Branch,
			PRID:       u.PRID,
			Coverage:   u.TotalPct,
			GateFailed: u.GateFailed,
			At:         u.CreatedAt,
		})
	}
	if d.Latest != nil {
		summary := &repoSummaryDTO{
			Verdict: verdictDTO{
				State:    d.Verdict.State,
				Coverage: d.Latest.TotalPct,
				Reason:   d.Verdict.Reason,
			},
			Commit: repoCommitDTO{
				UploadID:  d.Latest.UploadID,
				SHA:       d.Latest.CommitSHA,
				At:        d.Latest.CreatedAt,
				Branch:    d.Latest.Branch,
				PRID:      d.Latest.PRID,
				IsDefault: d.Latest.Branch == d.Repo.DefaultBranch,
			},
			CoveredStmts: d.Latest.CoveredStmts,
			TotalStmts:   d.Latest.TotalStmts,
		}
		if d.Base != nil {
			delta := d.Latest.TotalPct - d.Base.TotalPct
			summary.Verdict.Delta = &delta
			summary.Verdict.Base = &baseRefDTO{UploadID: d.Base.UploadID, SHA: d.Base.CommitSHA, Coverage: d.Base.TotalPct}
		}
		if d.LastUpload != nil {
			summary.LastUpload = &lastUploadDTO{At: d.LastUpload.CreatedAt, CILabel: d.LastProv.CILabel}
		}
		dto.Summary = summary
	}
	s.writeJSON(w, dto)
}

const (
	uploadsPageSize = 10
	// recentUploads bounds the newest-uploads fetch that fills the branch
	// selector, and doubles as the first pages' history without a second query.
	recentUploads = 100
	// trendReportLimit bounds the branch history behind the coverage trend
	// and the dashboard's sparklines.
	trendReportLimit = 60
)

// reportBaseline pairs a branch's newest merged report (reports come newest
// first) with the report it should be compared against — the most recent
// gate-passing report before it, the same baseline rule the upload API uses,
// so the UI never shows a delta measured against a report that failed the
// gate. base is nil when the branch has no earlier passing report (a single
// report, or a run of failures fills the window).
func reportBaseline(reports []*store.CommitReport) (current, base *store.CommitReport) {
	if len(reports) == 0 {
		return nil, nil
	}
	if i := slices.IndexFunc(reports[1:], func(cr *store.CommitReport) bool { return !cr.GateFailed }); i >= 0 {
		base = reports[1+i]
	}
	return reports[0], base
}

// branchBaseReport reads a branch's recent reports and pairs the newest with
// its baseline. Lookback is bounded; a branch whose last 50 reports all
// failed shows no delta.
func (s *Server) branchBaseReport(ctx context.Context, repoID int64, branch string) (current, base *store.CommitReport) {
	reports, err := s.store.ListBranchCommitReports(ctx, repoID, branch, 50)
	if err != nil {
		return nil, nil
	}
	return reportBaseline(reports)
}
