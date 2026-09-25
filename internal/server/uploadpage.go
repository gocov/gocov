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
	"iter"
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
		Verdict:      gateVerdict("This upload", u.TotalPct, u.DiffCoverage, u.GateFailed, repo.Gate, u.GateBasePct),
		CoveredStmts: u.CoveredStmts,
		TotalStmts:   u.TotalStmts,
		FileCount:    len(files.Files),
		Format:       u.Format,
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
	FileCount    int           `json:"file_count"`
	Format       string        `json:"format"`
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

	diffFiles := make(map[string]bool)
	if upload.DiffCoverage != nil {
		for _, df := range upload.DiffCoverage.Files {
			diffFiles[df.Path] = true
		}
		for _, uf := range upload.DiffCoverage.UnmatchedFiles {
			diffFiles[uf] = true
		}
	}

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
		row.SourceChanged = isSourceChanged(f.Path, upload.PathPrefix, diffFiles)
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

// isSourceChanged reports whether a profile path is one of the diff's files,
// matching the way diffcov pairs the two: exact (after the upload's path
// prefix) when a prefix is known, otherwise by a directory-aligned suffix in
// either direction. A bare file name never matches by suffix — "main.go"
// in the diff must not flag every main.go in the profile.
func isSourceChanged(fPath, pathPrefix string, diffFiles map[string]bool) bool {
	if diffFiles[fPath] {
		return true
	}
	if pathPrefix != "" {
		repoPath, _ := strings.CutPrefix(fPath, strings.TrimSuffix(pathPrefix, "/")+"/")
		return diffFiles[repoPath]
	}
	for dp := range diffFiles {
		if strings.Contains(dp, "/") && strings.HasSuffix(fPath, "/"+dp) {
			return true
		}
		if strings.Contains(fPath, "/") && strings.HasSuffix(dp, "/"+fPath) {
			return true
		}
	}
	return false
}

// gateVerdict states a coverage standing against the repo's gate, once at
// the top of the upload page (for that upload) and of the repo page (for
// the branch's newest merged report). The headline pass/fail follows the
// stored gate result; the reason narrates each configured rule against
// the values measured, so a reader sees why it stands. dropBase is the
// drop baseline the gate was judged against (GateBasePct), so the reason
// narrates the comparison the gate made rather than the page's own
// baseline; subject is how the reason names what was measured ("This
// upload").
func gateVerdict(subject string, totalPct float64, diff *diffcov.Result, gateFailed bool, gate store.Gate, dropBase *float64) verdictDTO {
	v := verdictDTO{State: "pass", Coverage: totalPct}
	switch {
	case !gate.Configured():
		v.State = "neutral"
	case gateFailed:
		v.State = "fail"
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

// uncoveredRanges formats the line ranges of never-executed blocks,
// e.g. "45-52, 88 +3 more".
func uncoveredRanges(blocks []profile.Block) string {
	merged := diffcov.MergedSpans(blocks, func(b profile.Block) bool { return b.Count == 0 && b.NumStmts > 0 })
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

// blockLines yields every line the blocks span, paired with the block
// spanning it, from line 1 up to at most limit — the length of a file whose
// content is at hand. Block ranges come from uploaders and may claim
// millions of lines, so there is no unbounded mode: code without a file
// length works on merged spans instead (diffcov.MergedSpans).
func blockLines(blocks []profile.Block, limit int) iter.Seq2[int, profile.Block] {
	return func(yield func(int, profile.Block) bool) {
		for _, b := range blocks {
			end := min(b.EndLine, limit)
			for l := max(b.StartLine, 1); l <= end; l++ {
				if !yield(l, b) {
					return
				}
			}
		}
	}
}

// lineCounts maps each line the blocks span, within the first limit lines,
// to the highest count of a block over it: a key means the line is
// executable, a positive value that it ran. The source view overlays this.
func lineCounts(blocks []profile.Block, limit int) map[int]int {
	counts := map[int]int{}
	for l, b := range blockLines(blocks, limit) {
		counts[l] = max(counts[l], b.Count)
	}
	return counts
}

// subtractSpans returns the lines of a not in b; both sorted and merged.
func subtractSpans(a, b []diffcov.Span) []diffcov.Span {
	var out []diffcov.Span
	j := 0
	for _, sp := range a {
		start := sp.Start
		for j < len(b) && b[j].End < start {
			j++
		}
		for k := j; k < len(b) && b[k].Start <= sp.End; k++ {
			if b[k].Start > start {
				out = append(out, diffcov.Span{Start: start, End: b[k].Start - 1})
			}
			start = max(start, b[k].End+1)
		}
		if start <= sp.End {
			out = append(out, diffcov.Span{Start: start, End: sp.End})
		}
	}
	return out
}

// intersectSpans returns the lines in both a and b; both sorted and merged.
func intersectSpans(a, b []diffcov.Span) []diffcov.Span {
	var out []diffcov.Span
	for i, j := 0, 0; i < len(a) && j < len(b); {
		if start, end := max(a[i].Start, b[j].Start), min(a[i].End, b[j].End); start <= end {
			out = append(out, diffcov.Span{Start: start, End: end})
		}
		if a[i].End < b[j].End {
			i++
		} else {
			j++
		}
	}
	return out
}

// newlyUncovered lists the lines a file executes-but-misses now that were hit
// at the baseline — the regressions this upload introduced, matched by line
// number. Best effort without a line-level diff, the same basis the source
// view uses to flag newly uncovered lines. A line is executable when a
// statement block spans it and hit when any such block ran; it works on
// spans rather than lines, since this renders on anonymous report pages
// from uploader-declared ranges.
func newlyUncovered(cur, base []profile.Block) string {
	stmts := func(b profile.Block) bool { return b.NumStmts > 0 }
	ran := func(b profile.Block) bool { return b.NumStmts > 0 && b.Count > 0 }
	missed := subtractSpans(diffcov.MergedSpans(cur, stmts), diffcov.MergedSpans(cur, ran))
	regressed := intersectSpans(missed, diffcov.MergedSpans(base, ran))
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
	name := fmt.Sprintf("%s-%s", shortSHA(upload.CommitSHA), profileFilename(upload.Format))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	if _, err := w.Write(raw); err != nil {
		s.log.Warn("writing raw profile", "upload", upload.ID, "err", err)
	}
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
