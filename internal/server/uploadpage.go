// The upload page (GET /uploads/{id}): what one upload actually reported
// — its verdict against the repo's gate, per-file coverage with the lines
// left uncovered, what changed against the previous upload, and where the
// upload came from. GET /uploads/{id}/profile hands back the raw profile
// it was built from.

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// handleUploadPage implements GET /uploads/{id} — the coverage verdict for
// one upload, the files it moved (before → after against the branch
// baseline) and the upload's provenance.
func (s *Server) handleUploadPage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.reportNotFound(w, r)
		return
	}
	upload, err := s.store.Upload(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.reportNotFound(w, r)
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
	if !s.authorizeReport(w, r, repo) {
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
		"PublicView":   s.publicView(r),
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
	v.Reason = core.GateReason(u.TotalPct, u.DiffCoverage, repo.Gate, baseTotal, base != nil, "This upload")
	return v
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
	// Tokenless marks an upload authenticated by workflow-run verification
	// instead of a token — rendered as "unverified contributor upload".
	Tokenless bool
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
		Tokenless:    m.Tokenless,
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

// splitPath separates a file path into its directory prefix (with trailing
// slash) and file name, so the table can dim the directory.
func splitPath(p string) (dir, base string) {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i+1], p[i+1:]
	}
	return "", p
}

// handleUploadProfile implements GET /uploads/{id}/profile — the raw coverage
// profile the upload was built from, served as an attachment. Same visibility
// rule as the upload page.
func (s *Server) handleUploadProfile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.reportNotFound(w, r)
		return
	}
	upload, err := s.store.Upload(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.reportNotFound(w, r)
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
	if !s.authorizeReport(w, r, repo) {
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
