// The upload page (GET /uploads/{id}): what one upload actually reported
// — its verdict against the repo's gate, per-file coverage with the lines
// left uncovered, what changed against the previous upload, and where the
// upload came from. GET /uploads/{id}/profile hands back the raw profile
// it was built from.

package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// handleUploadPage implements GET /uploads/{id}. Like the repo page it
// answers with the access decision and the head tags; the report itself
// is the UI API's to assemble.
func (s *Server) handleUploadPage(w http.ResponseWriter, r *http.Request) {
	upload, repo, ok := s.reportUpload(w, r)
	if !ok {
		return
	}
	s.serveApp(w, r, http.StatusOK, uploadPageHead(repo, upload))
}

// buildUploadPage assembles the upload page for the app: its verdict
// against the repo's gate, the files it moved against the baseline, and
// where it came from. A false second result means the answer is already
// written.
func (s *Server) buildUploadPage(w http.ResponseWriter, r *http.Request) (*uploadPageDTO, bool) {
	u, repo, ok := s.reportUpload(w, r)
	if !ok {
		return nil, false
	}

	// The baseline is the newest earlier gate-passing upload on the same
	// branch; its per-file coverage feeds the before → after column, and its
	// total feeds the headline delta — the same baseline the source view uses.
	base, baseFiles := s.baselineUpload(r.Context(), repo, u)
	files, err := s.buildFilesView(r.Context(), u, base, baseFiles)
	if err != nil {
		s.internalError(w, "loading upload files", err)
		return nil, false
	}
	dto := &uploadPageDTO{
		Repo: newRepoRefDTO(repo),
		Upload: uploadHeadDTO{
			ID:            u.ID,
			SHA:           u.CommitSHA,
			Branch:        u.Branch,
			PRID:          u.PRID,
			At:            u.CreatedAt,
			CommitMessage: u.Meta.CommitMessage,
			CommitAuthor:  u.Meta.CommitAuthor,
			Tokenless:     u.Meta.Tokenless,
		},
		Verdict:      gateVerdict("This upload", u.TotalPct, u.DiffCoverage, u.GateFailed, store.JudgedGate(u.Gate, repo.Gate), u.GateBasePct),
		CoveredStmts: u.CoveredStmts,
		TotalStmts:   u.TotalStmts,
		Files:        files,
		Provenance:   s.uploadProvenance(r.Context(), u),
	}
	if base != nil {
		dto.Verdict.against(base.ID, base.CommitSHA, base.TotalPct)
	}
	if dc := u.DiffCoverage; dc != nil {
		dto.Diff = &diffCovDTO{
			Coverage:       dc.Percent(),
			CoveredLines:   dc.CoveredLines,
			TotalLines:     dc.TotalLines,
			ChangedFiles:   len(dc.Files),
			UnmatchedFiles: len(dc.UnmatchedFiles),
		}
	}
	if u.RawBlobKey != "" {
		dto.DownloadURL = new(uploadProfileURL(u))
	}
	return dto, true
}

// uploadProfileURL is the raw-profile download route for an upload.
func uploadProfileURL(u *store.Upload) string {
	return fmt.Sprintf("/uploads/%d/profile", u.ID)
}

// uploadPageDTO is the upload page for the app.
type uploadPageDTO struct {
	Repo         repoRefDTO    `json:"repo"`
	Upload       uploadHeadDTO `json:"upload"`
	Verdict      verdictDTO    `json:"verdict"`
	CoveredStmts int64         `json:"covered_stmts"`
	TotalStmts   int64         `json:"total_stmts"`
	Diff         *diffCovDTO   `json:"diff"`
	Files        *filesViewDTO `json:"files"`
	Provenance   provenanceDTO `json:"provenance"`
	// DownloadURL is the raw profile's route, null when the upload kept no
	// profile to hand back.
	DownloadURL *string `json:"download_url"`
}

type uploadHeadDTO struct {
	ID            int64     `json:"id"`
	SHA           string    `json:"sha"`
	Branch        string    `json:"branch"`
	PRID          string    `json:"pr_id"`
	At            time.Time `json:"at"`
	CommitMessage string    `json:"commit_message"`
	CommitAuthor  string    `json:"commit_author"`
	Tokenless     bool      `json:"tokenless"`
}

// diffCovDTO is the PR diff's coverage; absent for uploads whose diff
// could not be fetched (or that are not PR builds at all).
type diffCovDTO struct {
	Coverage       float64 `json:"coverage"`
	CoveredLines   int64   `json:"covered_lines"`
	TotalLines     int64   `json:"total_lines"`
	ChangedFiles   int     `json:"changed_files"`
	UnmatchedFiles int     `json:"unmatched_files"`
}

// provenanceDTO is the Upload card: what we recorded about how this upload
// arrived. Every field degrades to empty for uploads made before the
// metadata was captured or through the raw API.
type provenanceDTO struct {
	ReceivedAt   time.Time `json:"received_at"`
	ProfileName  string    `json:"profile_name"`
	ProfileSize  string    `json:"profile_size"`
	Format       string    `json:"format"`
	CILabel      string    `json:"ci_label"` // "GitHub Actions", "GitLab CI", "Bitbucket Pipelines"
	CIRunURL     string    `json:"ci_run_url"`
	Uploader     string    `json:"uploader"`
	UploaderKind string    `json:"uploader_kind"` // "CLI" or "Action"
	Part         string    `json:"part"`          // the upload's part, "" for the default single profile
	PartsNote    string    `json:"parts_note"`    // "single profile, no merge" or "merged from N parts"
	Processed    string    `json:"processed"`     // server processing time, "" when not recorded
	Ignored      string    `json:"ignored"`       // "3 files ignored", "" when no pattern matched
}

// handleAPIUpload implements GET /api/ui/uploads/{id}.
func (s *Server) handleAPIUpload(w http.ResponseWriter, r *http.Request) {
	if dto, ok := s.buildUploadPage(w, r); ok {
		s.writeJSON(w, dto)
	}
}

// buildFilesView loads an upload's files and pairs each with its coverage
// at the baseline — the files card, shared by the upload page and the repo
// page. The directory tree the card draws is the client's to build from
// these rows. An upload without per-file data yields an empty card, not an
// error.
func (s *Server) buildFilesView(ctx context.Context, upload *store.Upload, base *store.Upload, baseFiles map[string]*store.UploadFile) (*filesViewDTO, error) {
	files, err := s.store.UploadFiles(ctx, upload.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	var diffPaths []string
	if dc := upload.DiffCoverage; dc != nil {
		for _, df := range dc.Files {
			diffPaths = append(diffPaths, df.Path)
		}
		diffPaths = append(diffPaths, dc.UnmatchedFiles...)
	}
	touched := diffcov.NewDiffPaths(diffPaths)

	// Each row keeps its delta and whether it changed at all, which order
	// the card but are not sent.
	type sortedRow struct {
		fileRowDTO
		delta   float64
		changed bool
	}
	rows := make([]sortedRow, 0, len(files))
	for _, f := range files {
		row := sortedRow{fileRowDTO: fileRowDTO{
			Path:         f.Path,
			Coverage:     f.Pct,
			CoveredStmts: f.CoveredStmts,
			TotalStmts:   f.TotalStmts,
			Uncovered:    uncoveredRanges(f.Blocks),
		}}
		if base != nil {
			if bf, ok := baseFiles[f.Path]; ok {
				row.Before = new(bf.Pct)
				row.BeforeCoveredStmts = new(bf.CoveredStmts)
				row.BeforeTotalStmts = new(bf.TotalStmts)
				row.delta = f.Pct - bf.Pct
				// A move too small to show as a percentage is not a change.
				if row.delta >= deltaEpsilon || row.delta <= -deltaEpsilon {
					row.CoverageChanged = true
				}
				if nm := newlyUncovered(f.Blocks, bf.Blocks); nm != "" {
					row.NewlyUncovered = nm
					row.CoverageChanged = true
				}
			} else {
				row.NewFile = true
				row.CoverageChanged = true
			}
		}
		row.SourceChanged = touched.Touches(f.Path, upload.PathPrefix)
		row.changed = row.CoverageChanged || row.SourceChanged
		rows = append(rows, row)
	}

	if base != nil {
		slices.SortStableFunc(rows, func(a, b sortedRow) int {
			if a.changed != b.changed {
				if a.changed {
					return -1
				}
				return 1
			}
			if a.changed {
				if c := cmp.Compare(a.delta, b.delta); c != 0 {
					return c
				}
			}
			return cmp.Compare(a.Path, b.Path)
		})
	}

	dto := &filesViewDTO{UploadID: upload.ID, HasBase: base != nil, Files: make([]fileRowDTO, len(rows))}
	for i, row := range rows {
		dto.Files[i] = row.fileRowDTO
	}
	return dto, nil
}

// deltaEpsilon is the smallest coverage move the UI shows as one: below
// it a file reads as unchanged rather than as a rounded-away "+0.0%".
const deltaEpsilon = 0.05

// gateState is a judged gate's outcome in one word: none when no rule
// was set, otherwise pass or fail.
func gateState(g store.Gate, failed bool) string {
	switch {
	case !g.Configured():
		return "none"
	case failed:
		return "fail"
	}
	return "pass"
}

// gateVerdict states a coverage standing against the gate it was judged
// by (store.JudgedGate), once at the top of the upload page (for that
// upload) and of the repo page (for the branch's newest merged report).
// The headline pass/fail follows the stored gate result; the reason
// narrates each rule against the values measured, so a reader sees why it
// stands. dropBase is the drop baseline the gate was judged against
// (GateBasePct), so the reason narrates the comparison the gate made
// rather than the page's own baseline; subject is how the reason names
// what was measured ("This upload").
func gateVerdict(subject string, totalPct float64, diff *diffcov.Result, gateFailed bool, gate store.Gate, dropBase *float64) verdictDTO {
	v := verdictDTO{State: gateState(gate, gateFailed), Coverage: totalPct}
	if v.State == "none" {
		v.State = "neutral" // the verdict card's word for it
	}
	v.Reason = core.GateReason(totalPct, diff, gate, dropBase, subject)
	return v
}

var ciLabels = map[string]string{
	"github":    "GitHub Actions",
	"gitlab":    "GitLab CI",
	"bitbucket": "Bitbucket Pipelines",
}

var uploaderKindLabels = map[string]string{"cli": "CLI", "action": "Action"}

// uploadProvenance builds the Upload card from the upload's captured metadata,
// resolving how many parts merged into the commit for the flags line.
func (s *Server) uploadProvenance(ctx context.Context, u *store.Upload) provenanceDTO {
	m := u.Meta
	p := provenanceDTO{
		ReceivedAt:   u.CreatedAt,
		ProfileName:  cmp.Or(m.ProfileName, profileFilename(u.Format)),
		Format:       u.Format,
		CILabel:      ciLabels[m.CIProvider],
		CIRunURL:     m.CIRunURL,
		Uploader:     m.Uploader,
		UploaderKind: uploaderKindLabels[m.UploaderKind],
	}
	if m.ProfileBytes > 0 {
		p.ProfileSize = humanBytes(m.ProfileBytes)
	}
	if u.Part != "" && u.Part != "default" {
		p.Part = u.Part
	}
	switch n := m.IgnoredFiles; {
	case n == 1:
		p.Ignored = "1 file ignored"
	case n > 1:
		p.Ignored = fmt.Sprintf("%d files ignored", n)
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

// maxUncoveredRanges caps the ranges shown per file in the table.
const maxUncoveredRanges = 6

// uncoveredRanges formats the ranges of executable lines nothing ran over
// (diffcov.MissedSpans), e.g. "45-52, 88 +3 more".
func uncoveredRanges(blocks []profile.Block) string {
	merged := diffcov.MissedSpans(blocks)
	if len(merged) == 0 {
		return ""
	}

	var parts []string
	for i, sp := range merged {
		if i == maxUncoveredRanges {
			parts = append(parts, fmt.Sprintf("+%d more", len(merged)-maxUncoveredRanges))
			break
		}
		parts = append(parts, sp.String())
	}
	return strings.Join(parts, ", ")
}

// lineCounts maps each line a statement block spans, from line 1 up to at
// most limit, to the highest count of such a block over it: a key means
// the line is executable, a positive value that it ran — diffcov's line
// rule, line by line. The source view overlays this.
// limit is the length of a file whose content is at hand: block ranges
// come from uploaders and may claim millions of lines, so there is no
// unbounded mode — code without a file length works on merged spans
// instead (diffcov.MergedSpans).
func lineCounts(blocks []profile.Block, limit int) map[int]int {
	counts := map[int]int{}
	for _, b := range blocks {
		if !diffcov.Statement(b) {
			continue
		}
		for l := max(b.StartLine, 1); l <= min(b.EndLine, limit); l++ {
			counts[l] = max(counts[l], b.Count)
		}
	}
	return counts
}

// newlyUncovered lists the lines a file executes-but-misses now that were hit
// at the baseline — the regressions this upload introduced, matched by line
// number. Best effort without a line-level diff, the same basis the source
// view uses to flag newly uncovered lines, and diffcov's line rule. It works
// on spans rather than lines, since this renders on anonymous report pages
// from uploader-declared ranges.
func newlyUncovered(cur, base []profile.Block) string {
	regressed := diffcov.IntersectSpans(diffcov.MissedSpans(cur), diffcov.MergedSpans(base, diffcov.Ran))
	parts := make([]string, len(regressed))
	for i, sp := range regressed {
		parts[i] = sp.String()
	}
	return strings.Join(parts, ", ")
}

// handleUploadProfile implements GET /uploads/{id}/profile — the raw coverage
// profile the upload was built from, served as an attachment. Same visibility
// rule as the upload page.
func (s *Server) handleUploadProfile(w http.ResponseWriter, r *http.Request) {
	upload, _, ok := s.reportUpload(w, r)
	if !ok {
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
	name := fmt.Sprintf("%s-%s", core.ShortSHA(upload.CommitSHA), profileFilename(upload.Format))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	if _, err := w.Write(raw); err != nil {
		s.log.Warn("writing raw profile", "upload", upload.ID, "err", err)
	}
}

// profileFilename is the conventional filename of an upload's raw report,
// used for the download's Content-Disposition.
func profileFilename(format string) string {
	if f, ok := profile.Lookup(format); ok && f.Filename != "" {
		return f.Filename
	}
	if format == "" {
		return "coverage.txt"
	}
	return "coverage." + format
}
