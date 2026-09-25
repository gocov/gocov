// The repo page (GET /repos/{slug...}): one repo's current verdict, its
// coverage trend, the files behind its latest commit (every part merged),
// and a paged list of the uploads behind it.

package server

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// handleRepo implements GET /repos/{forge}/{slug...}. The page
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
// selected branch, the trend behind it and the files of its latest report.
// The upload history pages through its own endpoint (handleAPIRepoUploads), so
// turning a page reads only the uploads. A false second result means the
// answer — a not-found, a refusal or an internal error — is already
// written.
func (s *Server) buildRepoPage(w http.ResponseWriter, r *http.Request) (*repoPageDTO, bool) {
	repo, member, ok := s.reportRepo(w, r)
	if !ok {
		return nil, false
	}

	branch := r.FormValue("branch")

	// The page's reads are independent of each other, so they run side by
	// side: the recent branches (the branch selector), the trend with the
	// files view hanging off its latest report, and the settings button's
	// workspace lookup.
	// The trend follows the page's branch filter, defaulting to the
	// repo's default branch when "All branches" is selected.
	trendBranch := cmp.Or(branch, repo.DefaultBranch)
	var (
		wg           sync.WaitGroup
		branches     []string
		branchesErr  error
		trendReports []*store.CommitReport
		trendErr     error
		latest, base *store.CommitReport
		lastUpload   *store.Upload
		files        *filesViewDTO
		canSettings  bool
	)
	wg.Go(func() {
		branches, branchesErr = s.store.RecentBranches(r.Context(), repo.ID, recentUploads)
	})
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
		var fw sync.WaitGroup
		fw.Go(func() {
			lu, err := s.store.Upload(r.Context(), latest.UploadID)
			if err != nil {
				s.log.Warn("loading latest upload for repo page", "upload", latest.UploadID, "err", err)
				return
			}
			lastUpload = lu
		})
		fw.Go(func() {
			var err error
			if files, err = s.loadCommitFilesView(r.Context(), repo, latest, base); err != nil {
				s.log.Warn("loading files for repo page", "commit", latest.CommitSHA, "err", err)
			}
		})
		fw.Wait()
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

	if branchesErr != nil {
		s.internalError(w, "listing recent branches", branchesErr)
		return nil, false
	}
	if trendErr != nil {
		s.internalError(w, "listing reports for trend", trendErr)
		return nil, false
	}

	dto := &repoPageDTO{
		Repo: repoHeadDTO{
			repoRefDTO:    newRepoRefDTO(repo),
			DefaultBranch: repo.DefaultBranch,
			Gate:          newGateDTO(repo.Gate),
			CanSettings:   canSettings,
		},
		Branches:    branches,
		TrendBranch: trendBranch,
		Trend:       []trendPointDTO{},
	}
	if dto.Branches == nil {
		dto.Branches = []string{}
	}
	// The trend reads oldest first. It is the branch's history as the store
	// keeps it — the default branch's without PR builds, a feature branch's
	// with its PR's builds — the same reports the summary above describes.
	for _, report := range slices.Backward(trendReports) {
		dto.Trend = append(dto.Trend, trendPointDTO{
			UploadID:   report.UploadID,
			SHA:        report.CommitSHA,
			Coverage:   report.TotalPct,
			At:         report.CreatedAt,
			GateFailed: report.GateFailed,
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
		summary.LastUpload = &lastUploadDTO{At: lastUpload.CreatedAt, CIProvider: lastUpload.Meta.CIProvider}
	}
	dto.Files = files
	dto.Summary = summary
	return dto, true
}

// loadCommitFilesView builds the repo page's files card from the latest
// commit's merged report: the files of every part's latest upload, merged
// the way the recompute merged the totals, against the baseline commit's
// parts merged the same way. A commit uploaded in parts (backend,
// frontend) lists every part's files, not only the last part in.
func (s *Server) loadCommitFilesView(ctx context.Context, repo *store.Repo, latest, base *store.CommitReport) (*filesViewDTO, error) {
	var (
		wg        sync.WaitGroup
		uploads   map[int64]*store.Upload
		files     []*store.UploadFile
		err       error
		baseFiles map[string]*store.UploadFile
	)
	wg.Go(func() { uploads, files, err = s.commitFiles(ctx, repo.ID, latest.CommitSHA) })
	if base != nil {
		// The comparison is decoration, never worth failing the card over.
		wg.Go(func() {
			_, bf, berr := s.commitFiles(ctx, repo.ID, base.CommitSHA)
			if berr != nil {
				s.log.Warn("loading baseline files for repo page", "commit", base.CommitSHA, "err", berr)
				return
			}
			baseFiles = make(map[string]*store.UploadFile, len(bf))
			for _, f := range bf {
				baseFiles[f.Path] = f
			}
		})
	}
	wg.Wait()
	if err != nil {
		return nil, err
	}
	return buildFilesView(latest.UploadID, latest.DiffCoverage, uploads, files, baseFiles != nil, baseFiles), nil
}

// commitFiles reads a commit's merged files — the files of the latest
// upload of each of its parts, keyed by id, with the files merged by path
// (mergePartFiles).
func (s *Server) commitFiles(ctx context.Context, repoID int64, commitSHA string) (map[int64]*store.Upload, []*store.UploadFile, error) {
	parts, err := s.store.LatestUploadsPerPart(ctx, repoID, commitSHA)
	if err != nil {
		return nil, nil, err
	}
	uploads := make(map[int64]*store.Upload, len(parts))
	ids := make([]int64, len(parts))
	for i, p := range parts {
		uploads[p.ID] = p
		ids[i] = p.ID
	}
	var files []*store.UploadFile
	switch len(ids) {
	case 0:
	case 1:
		files, err = s.store.UploadFiles(ctx, ids[0])
	default:
		files, err = s.store.PartFiles(ctx, ids)
	}
	if err != nil {
		return nil, nil, err
	}
	return uploads, mergePartFiles(files), nil
}

// mergePartFiles merges the files of a commit's parts by path, ordered by
// path. A file only one part reports is kept as it is; one several parts
// report has its blocks merged (profile.Merge, the recompute's rule: a line
// any part ran is covered) and belongs to the newest of those uploads.
func mergePartFiles(files []*store.UploadFile) []*store.UploadFile {
	byPath := make(map[string][]*store.UploadFile, len(files))
	for _, f := range files {
		byPath[f.Path] = append(byPath[f.Path], f)
	}
	out := make([]*store.UploadFile, 0, len(byPath))
	for path, same := range byPath {
		if len(same) == 1 {
			out = append(out, same[0])
			continue
		}
		profiles := make([]*profile.Profile, len(same))
		var owner int64
		for i, f := range same {
			profiles[i] = &profile.Profile{Files: []profile.File{{Path: path, Blocks: f.Blocks}}}
			owner = max(owner, f.UploadID)
		}
		merged := profile.Merge(profiles...).Files[0]
		covered, total := merged.Coverage()
		out = append(out, &store.UploadFile{
			UploadID:     owner,
			Path:         path,
			Pct:          profile.Percent(covered, total),
			CoveredStmts: covered,
			TotalStmts:   total,
			Blocks:       merged.Blocks,
		})
	}
	slices.SortFunc(out, func(a, b *store.UploadFile) int { return cmp.Compare(a.Path, b.Path) })
	return out
}

// repoPageDTO is the repo page for the app. The trend arrives as raw
// points and the files as a flat list: the chart's geometry and the
// directory tree are the client's to build.
type repoPageDTO struct {
	Repo        repoHeadDTO     `json:"repo"`
	Branches    []string        `json:"branches"`
	TrendBranch string          `json:"trend_branch"`
	Summary     *repoSummaryDTO `json:"summary"`
	Trend       []trendPointDTO `json:"trend"`
	Files       *filesViewDTO   `json:"files"`
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
	At         time.Time `json:"at"`
	CIProvider string    `json:"ci_provider"` // "github", "gitlab", "bitbucket"; "" when unknown
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

// repoUploadsDTO is one page of the repo page's upload history.
type repoUploadsDTO struct {
	Uploads  []uploadRowDTO `json:"uploads"`
	HasOlder bool           `json:"has_older"`
}

// handleAPIRepoUploads implements GET
// /api/ui/repo-uploads/{forge}/{slug...}?branch=&page=, the repo page's
// upload history. It answers to the same access decision as the page.
func (s *Server) handleAPIRepoUploads(w http.ResponseWriter, r *http.Request) {
	repo, _, ok := s.reportRepo(w, r)
	if !ok {
		return
	}
	branch := r.FormValue("branch")
	page, _ := strconv.Atoi(r.FormValue("page"))
	if page < 0 {
		page = 0
	}

	// Fetch the page and one row beyond it, so "Older" knows whether to
	// render.
	offset, limit := page*uploadsPageSize, uploadsPageSize+1
	var (
		fetched []*store.Upload
		err     error
	)
	if branch == "" {
		fetched, err = s.store.ListUploads(r.Context(), repo.ID, offset, limit)
	} else {
		fetched, err = s.store.ListBranchUploads(r.Context(), repo.ID, branch, offset, limit)
	}
	if err != nil {
		s.internalError(w, "listing uploads", err)
		return
	}
	shown := fetched[:min(len(fetched), uploadsPageSize)]
	dto := &repoUploadsDTO{
		Uploads:  make([]uploadRowDTO, 0, len(shown)),
		HasOlder: len(fetched) > uploadsPageSize,
	}
	for _, u := range shown {
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
	s.writeJSON(w, dto)
}

const (
	uploadsPageSize = 10
	// recentUploads bounds the newest-uploads fetch that fills the branch
	// selector.
	recentUploads = 100
	// trendReportLimit bounds the branch history behind the coverage trend
	// and the dashboard's sparklines.
	trendReportLimit = 60
	// baselineLookback bounds the search for a comparison baseline; a
	// branch whose last 50 reports all failed shows no delta.
	baselineLookback = 50
)
