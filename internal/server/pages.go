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

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

const uploadsPageSize = 25

// deltaView is a precomputed coverage delta for the templates.
type deltaView struct {
	Class string // up, down, flat
	Arrow string
	Text  string
}

func newDeltaView(d float64) *deltaView {
	switch {
	case d >= 0.05:
		return &deltaView{"up", "▲", fmt.Sprintf("%+.1f%%", d)}
	case d <= -0.05:
		return &deltaView{"down", "▼", fmt.Sprintf("%+.1f%%", d)}
	default:
		return &deltaView{"flat", "—", "0.0%"}
	}
}

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

// handleIndex implements GET / — the workspace-scoped repo dashboard: a
// switcher over the viewer's workspaces, a stat rollup, the needs-attention
// list and the repositories table. See dashboard.go for the assembly.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	scope, err := s.userScope(r)
	if err != nil {
		s.internalError(w, "scoping repos", err)
		return
	}
	// A hosted user without a single workspace membership would see a
	// permanently empty dashboard; registration is the only useful page
	// for them (M3/R1).
	if s.hosted && scope.scoped && len(scope.prefixes) == 0 && currentUser(r) != nil {
		http.Redirect(w, r, "/onboarding", http.StatusFound)
		return
	}
	dash, err := s.buildDashboard(r, strings.TrimSpace(r.FormValue("ws")))
	if err != nil {
		s.internalError(w, "building dashboard", err)
		return
	}
	s.render(w, r, "index.html", map[string]any{
		"Dash": dash,
		// A signed-in user can register a workspace from the onboarding
		// wizard (hosted and private mode alike); an open instance has no
		// identity to register from and points at sign-in instead.
		"CanOnboard": currentUser(r) != nil,
	})
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
	v.Reason = gateReason(latest.TotalPct, latest.DiffCoverage, repo.Gate, baseTotal, base != nil, "The latest commit")
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
	for _, prefix := range slugPrefixes(repo.Slug) { // longest (most specific) first
		for _, ws := range workspaces {
			if ws.Forge == repo.Forge && ws.Prefix == prefix {
				return prefix
			}
		}
	}
	return ""
}

// handleRepo implements GET /repos/{workspace}/{repo} — stats, badge embed,
// branch filter and the upload list.
func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	repo, err := s.store.RepoBySlug(r.Context(), slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, "loading repo", err)
		return
	}
	if ok, err := s.canView(r, repo.Slug); err != nil {
		s.internalError(w, "checking access", err)
		return
	} else if !ok {
		http.NotFound(w, r)
		return
	}

	branch := r.FormValue("branch")
	page, _ := strconv.Atoi(r.FormValue("page"))
	if page < 0 {
		page = 0
	}

	// Fetch one page beyond the current one so "Older" knows whether to
	// render; the recent list also feeds the branch selector.
	recent, err := s.store.ListUploads(r.Context(), repo.ID, 100)
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
		if limit <= 101 {
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

	s.render(w, r, "repo.html", map[string]any{
		"Repo":        repo,
		"Latest":      latest,
		"Verdict":     verdict,
		"Uncovered":   uncovered,
		"LastUpload":  lastProv,
		"Miss":        miss,
		"WSPrefix":    s.repoWorkspacePrefix(r.Context(), repo),
		"GateSummary": gateSummary(repo.Gate),
		"Branches":    branches,
		"Branch":      branch,
		"TrendBranch": trendBranch,
		"Trend":       trend,
		"Uploads":     uploads,
		"Page":        page,
		"PrevPage":    page - 1,
		"NextPage":    page + 1,
		"HasOlder":    hasOlder,
		"BaseURL":     strings.TrimSuffix(s.baseURL, "/"),
	})
}

// uploadFileRow decorates a stored file with its coverage history for the
// upload detail table: the same file's coverage at the branch baseline, the
// resulting delta, and the lines this upload newly left uncovered. The
// baseline fields are empty when there is no baseline to compare against.
type uploadFileRow struct {
	*store.UploadFile
	Dir, Base string     // path split for display (directory prefix, file name)
	Uncovered string     // all-time uncovered ranges, shown when there is no baseline
	HasBefore bool       // the file existed at the baseline
	BeforeStr string     // baseline coverage, preformatted
	Delta     *deltaView // after − before
	DeltaVal  float64    // after − before, for ordering
	NewFile   bool       // absent from the baseline upload
	NewlyMiss string     // ranges covered at the baseline but uncovered now
	Changed   bool       // coverage moved, the file is new, or it regressed
}

// maxUncoveredRanges caps the ranges shown per file in the table.
const maxUncoveredRanges = 6

// uncoveredRanges formats the line ranges of never-executed blocks,
// e.g. "45-52, 88 +3 more".
func uncoveredRanges(blocks []profile.Block) string {
	type span struct{ start, end int }
	var spans []span
	for _, b := range blocks {
		if b.Count > 0 || b.NumStmts == 0 {
			continue
		}
		spans = append(spans, span{b.StartLine, b.EndLine})
	}
	if len(spans) == 0 {
		return ""
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	merged := spans[:1]
	for _, sp := range spans[1:] {
		last := &merged[len(merged)-1]
		if sp.start <= last.end+1 {
			if sp.end > last.end {
				last.end = sp.end
			}
			continue
		}
		merged = append(merged, sp)
	}

	var parts []string
	for i, sp := range merged {
		if i == maxUncoveredRanges {
			parts = append(parts, fmt.Sprintf("+%d more", len(merged)-maxUncoveredRanges))
			break
		}
		if sp.start == sp.end {
			parts = append(parts, strconv.Itoa(sp.start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", sp.start, sp.end))
		}
	}
	return strings.Join(parts, ", ")
}

// handleUploadPage implements GET /uploads/{id} — the coverage verdict for
// one upload, the files it moved (before → after against the branch
// baseline) and the upload's provenance.
func (s *Server) handleUploadPage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	upload, err := s.store.Upload(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, "loading upload", err)
		return
	}
	files, err := s.store.UploadFiles(r.Context(), id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, "loading upload files", err)
		return
	}
	repo, err := s.store.RepoByID(r.Context(), upload.RepoID)
	if err != nil {
		s.internalError(w, "loading repo for upload", err)
		return
	}
	if ok, err := s.canView(r, repo.Slug); err != nil {
		s.internalError(w, "checking access", err)
		return
	} else if !ok {
		http.NotFound(w, r)
		return
	}

	// The baseline is the newest earlier gate-passing upload on the same
	// branch; its per-file coverage feeds the before → after column, and its
	// total feeds the headline delta — the same baseline the source view uses.
	base, baseFiles := s.baselineUpload(r.Context(), repo, upload)

	rows := make([]uploadFileRow, 0, len(files))
	for _, f := range files {
		dir, name := splitPath(f.Path)
		row := uploadFileRow{UploadFile: f, Dir: dir, Base: name, Uncovered: uncoveredRanges(f.Blocks)}
		if base != nil {
			if bf, ok := baseFiles[f.Path]; ok {
				row.HasBefore = true
				row.BeforeStr = fmt.Sprintf("%.1f%%", bf.Pct)
				row.DeltaVal = f.Pct - bf.Pct
				row.Delta = newDeltaView(row.DeltaVal)
				if row.Delta.Class != "flat" {
					row.Changed = true
				}
				if nm := newlyUncovered(f.Blocks, bf.Blocks); nm != "" {
					row.NewlyMiss = nm
					row.Changed = true
				}
			} else {
				row.NewFile = true
				row.Changed = true
			}
		}
		rows = append(rows, row)
	}
	// With a baseline, surface the files this upload moved first — biggest
	// drops at the top — then the rest alphabetically. Without one, keep the
	// store's path order.
	changed := 0
	if base != nil {
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Changed != rows[j].Changed {
				return rows[i].Changed
			}
			if rows[i].Changed && rows[i].DeltaVal != rows[j].DeltaVal {
				return rows[i].DeltaVal < rows[j].DeltaVal
			}
			return rows[i].Path < rows[j].Path
		})
		for _, row := range rows {
			if row.Changed {
				changed++
			}
		}
	}

	s.render(w, r, "upload.html", map[string]any{
		"Upload":  upload,
		"Repo":    repo,
		"Files":   rows,
		"HasBase": base != nil,
		// Touched: the commit moved at least one file's coverage, so the
		// table leads with those. Collapse: there are also unchanged files to
		// tuck behind a "show all" toggle.
		"Touched":      base != nil && changed > 0,
		"Collapse":     base != nil && changed > 0 && changed < len(rows),
		"ChangedCount": changed,
		"Verdict":      s.uploadVerdict(upload, repo, base, len(files)),
		"Prov":         s.uploadProvenance(r.Context(), upload),
		"CanDownload":  upload.RawBlobKey != "",
		"Download":     fmt.Sprintf("/uploads/%d/profile", upload.ID),
	})
}

// splitPath separates a file path into its directory prefix (with trailing
// slash) and file name, so the table can dim the directory.
func splitPath(p string) (dir, base string) {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i+1], p[i+1:]
	}
	return "", p
}

// verdictView is the coverage verdict rendered at the top of the upload page.
type verdictView struct {
	State      string // "pass", "fail" or "neutral" (no gate configured)
	Pct        float64
	CovClass   string
	Delta      *deltaView // total coverage vs the baseline
	Statements string     // "1,240 of 1,473"
	FileCount  int
	Format     string
	BaseID     int64
	BaseSHA    string
	BasePctStr string
	Reason     string // prose walk-through of the gate rules and their outcome
}

// uploadVerdict assembles the verdict card. The headline pass/fail follows the
// upload's stored gate result; the reason narrates each configured rule
// against the values this upload measured, so a reader sees why it stands.
func (s *Server) uploadVerdict(u *store.Upload, repo *store.Repo, base *store.Upload, fileCount int) verdictView {
	v := verdictView{
		Pct:        u.TotalPct,
		CovClass:   covClass(u.TotalPct),
		Statements: fmt.Sprintf("%s of %s", humanInt(u.CoveredStmts), humanInt(u.TotalStmts)),
		FileCount:  fileCount,
		Format:     u.Format,
	}
	var baseTotal float64
	if base != nil {
		v.BaseID = base.ID
		v.BaseSHA = base.CommitSHA
		v.BasePctStr = fmt.Sprintf("%.1f%%", base.TotalPct)
		v.Delta = newDeltaView(u.TotalPct - base.TotalPct)
		baseTotal = base.TotalPct
	}
	switch {
	case !repo.Gate.Configured():
		v.State = "neutral"
	case u.GateFailed:
		v.State = "fail"
	default:
		v.State = "pass"
	}
	v.Reason = gateReason(u.TotalPct, u.DiffCoverage, repo.Gate, baseTotal, base != nil, "This upload")
	return v
}

// gateReason narrates the gate: one clause per configured rule, comparing this
// upload's measured value to the threshold, joined into a sentence. It reads
// the same whether the gate passed or failed — the clauses themselves say
// which rule is the problem.
// subject names the thing being described in the fallback sentences (e.g.
// "This upload", "The latest commit on this branch") so the same narration
// serves both the upload page and the repo page. totalPct/diff are the
// measured values; baseTotal is the baseline's total coverage, valid only
// when hasBase is true.
func gateReason(totalPct float64, diff *diffcov.Result, g store.Gate, baseTotal float64, hasBase bool, subject string) string {
	if !g.Configured() {
		return fmt.Sprintf("No coverage gate is configured for this repo. %s records %.1f%% total coverage.", subject, totalPct)
	}
	var parts []string
	if g.MinCoverage != nil {
		rel := "is above"
		if totalPct < *g.MinCoverage-gateEpsilon {
			rel = "is below"
		}
		parts = append(parts, fmt.Sprintf("total coverage %s the minimum of %.4g%%", rel, *g.MinCoverage))
	}
	if g.MaxCoverageDrop != nil && hasBase {
		drop := baseTotal - totalPct
		if drop <= gateEpsilon {
			parts = append(parts, "coverage held or rose against the base")
		} else {
			rel := "under"
			if drop > *g.MaxCoverageDrop+gateEpsilon {
				rel = "over"
			}
			parts = append(parts, fmt.Sprintf("the drop against the base is %.4g%% — %s the %.4g%% allowed", drop, rel, *g.MaxCoverageDrop))
		}
	}
	if g.MinDiffCoverage != nil && diff != nil && diff.TotalLines > 0 {
		rel := "meets"
		if diff.Percent() < *g.MinDiffCoverage-gateEpsilon {
			rel = "is below"
		}
		parts = append(parts, fmt.Sprintf("diff coverage %s the %.4g%% minimum", rel, *g.MinDiffCoverage))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%s records %.1f%% total coverage.", subject, totalPct)
	}
	sentence := strings.ToUpper(parts[0][:1]) + parts[0][1:]
	if len(parts) > 1 {
		sentence += ", and " + strings.Join(parts[1:], ", and ")
	}
	return sentence + "."
}

// provView is the Upload provenance card: what we recorded about how this
// upload arrived. Every field degrades to empty for uploads made before the
// metadata was captured or through the raw API.
type provView struct {
	Received     string
	Ago          string
	ProfileName  string
	ProfileSize  string
	Format       string
	CILabel      string // "GitHub Actions", "GitLab CI", "Bitbucket Pipelines"
	CIRunURL     string
	Uploader     string
	UploaderKind string // "CLI" or "Action"
	Part         string // the upload's part, "" for the default single profile
	PartsNote    string // "single profile, no merge" or "merged from N parts"
	Processed    string // server processing time, "" when not recorded
}

var ciLabels = map[string]string{
	"github":    "GitHub Actions",
	"gitlab":    "GitLab CI",
	"bitbucket": "Bitbucket Pipelines",
}

var uploaderKindLabels = map[string]string{"cli": "CLI", "action": "Action"}

// uploadProvenance builds the Upload card from the upload's captured metadata,
// resolving how many parts merged into the commit for the flags line.
func (s *Server) uploadProvenance(ctx context.Context, u *store.Upload) provView {
	m := u.Meta
	p := provView{
		Received:     u.CreatedAt.UTC().Format("2 Jan 2006, 15:04 UTC"),
		Ago:          timeAgo(u.CreatedAt),
		ProfileName:  m.ProfileName,
		Format:       u.Format,
		CILabel:      ciLabels[m.CIProvider],
		CIRunURL:     m.CIRunURL,
		Uploader:     m.Uploader,
		UploaderKind: uploaderKindLabels[m.UploaderKind],
	}
	if p.ProfileName == "" {
		p.ProfileName = profileFilename(u.Format)
	}
	if m.ProfileBytes > 0 {
		p.ProfileSize = humanBytes(m.ProfileBytes)
	}
	if u.Part != "" && u.Part != "default" {
		p.Part = u.Part
	}
	switch ms := m.ProcessMillis; {
	case ms >= 1000:
		p.Processed = fmt.Sprintf("%.1f s", float64(ms)/1000)
	case ms > 0:
		p.Processed = fmt.Sprintf("%d ms", ms)
	}
	// Count the parts that fed the commit for the flags line.
	if parts, err := s.store.CommitParts(ctx, u.RepoID, u.CommitSHA); err == nil && len(parts) > 1 {
		p.PartsNote = fmt.Sprintf("merged from %d parts", len(parts))
	} else {
		p.PartsNote = "single profile, no merge"
	}
	return p
}

// humanBytes formats a byte count as a compact size.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// humanInt formats a statement count with thousands separators.
func humanInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// lineCoverage returns the executable and hit line sets for a file's blocks:
// a line is executable when a statement block spans it, and hit when any such
// block ran. The same rule the source view overlays.
func lineCoverage(blocks []profile.Block) (exec, hit map[int]bool) {
	exec = map[int]bool{}
	hit = map[int]bool{}
	for _, b := range blocks {
		if b.NumStmts == 0 {
			continue
		}
		for l := max(b.StartLine, 1); l <= b.EndLine; l++ {
			exec[l] = true
			if b.Count > 0 {
				hit[l] = true
			}
		}
	}
	return exec, hit
}

// newlyUncovered lists the lines a file executes-but-misses now that were hit
// at the baseline — the regressions this upload introduced, matched by line
// number. Best effort without a line-level diff, the same basis the source
// view uses to flag newly uncovered lines.
func newlyUncovered(cur, base []profile.Block) string {
	exec, hit := lineCoverage(cur)
	_, baseHit := lineCoverage(base)
	var lines []int
	for l := range exec {
		if !hit[l] && baseHit[l] {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	sort.Ints(lines)
	return diffcov.Ranges(lines)
}

// profileFilenames maps a profile format to the conventional filename of the
// raw report, used for the download's Content-Disposition.
var profileFilenames = map[string]string{
	"go":        "coverage.out",
	"lcov":      "lcov.info",
	"jacoco":    "jacoco.xml",
	"cobertura": "cobertura.xml",
}

func profileFilename(format string) string {
	if n, ok := profileFilenames[format]; ok {
		return n
	}
	if format == "" {
		return "coverage.txt"
	}
	return "coverage." + format
}

// handleUploadProfile implements GET /uploads/{id}/profile — the raw coverage
// profile the upload was built from, served as an attachment. Same visibility
// rule as the upload page.
func (s *Server) handleUploadProfile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	upload, err := s.store.Upload(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, "loading upload", err)
		return
	}
	repo, err := s.store.RepoByID(r.Context(), upload.RepoID)
	if err != nil {
		s.internalError(w, "loading repo for upload", err)
		return
	}
	if ok, err := s.canView(r, repo.Slug); err != nil {
		s.internalError(w, "checking access", err)
		return
	} else if !ok {
		http.NotFound(w, r)
		return
	}
	if upload.RawBlobKey == "" {
		http.NotFound(w, r)
		return
	}
	raw, err := s.blobs.Get(r.Context(), upload.RawBlobKey)
	if err != nil {
		s.internalError(w, "loading raw profile", err)
		return
	}
	name := fmt.Sprintf("%s-%s", shortSHA(upload.CommitSHA), profileFilename(upload.Format))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	if _, err := w.Write(raw); err != nil {
		s.log.Warn("writing raw profile", "upload", id, "err", err)
	}
}
