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
	"maps"
	"net/http"
	"slices"
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
	repo, err := s.store.RepoByID(r.Context(), upload.RepoID)
	if err != nil {
		s.internalError(w, "loading repo for upload", err)
		return
	}
	if _, ok := s.authorizeReport(w, r, repo); !ok {
		return
	}

	// The baseline is the newest earlier gate-passing upload on the same
	// branch; its per-file coverage feeds the before → after column, and its
	// total feeds the headline delta — the same baseline the source view uses.
	base, baseFiles := s.baselineUpload(r.Context(), repo, upload)
	fv, err := s.buildFilesViewData(r.Context(), upload, base, baseFiles, "")
	if err != nil {
		s.internalError(w, "loading upload files", err)
		return
	}

	s.render(w, r, "upload.html", map[string]any{
		"Upload":      upload,
		"Repo":        repo,
		"FilesView":   fv,
		"Verdict":     s.uploadVerdict(upload, repo, base, fv.TotalFiles),
		"Prov":        s.uploadProvenance(r.Context(), upload),
		"CanDownload": upload.RawBlobKey != "",
		"Download":    fmt.Sprintf("/uploads/%d/profile", upload.ID),
		"PublicView":  s.publicView(r),
	})
}

// filesViewData is the model behind the "filesview" partial — the files
// card with its Tree/List switch and filter tabs — shared by the upload page
// and the repo page.
type filesViewData struct {
	UploadID   int64
	Files      []uploadFileRow
	TreeRows   []treeRow
	Counts     fileCounts
	HasBase    bool
	TotalFiles int
	Heading    string
}

// buildFilesViewData loads an upload's files and builds the flat list and
// the directory tree with their filter counts and baseline comparisons. An
// upload without per-file data yields an empty view, not an error.
func (s *Server) buildFilesViewData(ctx context.Context, upload *store.Upload, base *store.Upload, baseFiles map[string]*store.UploadFile, heading string) (filesViewData, error) {
	files, err := s.store.UploadFiles(ctx, upload.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return filesViewData{}, err
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

	rows := make([]uploadFileRow, 0, len(files))
	for _, f := range files {
		dir, name := splitPath(f.Path)
		row := uploadFileRow{UploadFile: f, Dir: dir, Base: name, Uncovered: uncoveredRanges(f.Blocks)}
		if base != nil {
			if bf, ok := baseFiles[f.Path]; ok {
				row.HasBefore = true
				row.BeforeCovered = bf.CoveredStmts
				row.BeforeTotal = bf.TotalStmts
				row.BeforeStr = fmt.Sprintf("%.1f%%", bf.Pct)
				row.DeltaVal = f.Pct - bf.Pct
				row.Delta = newDeltaView(row.DeltaVal)
				if row.Delta.Class != "flat" {
					row.IsCoverageChanged = true
				}
				if nm := newlyUncovered(f.Blocks, bf.Blocks); nm != "" {
					row.NewlyMiss = nm
					row.IsCoverageChanged = true
				}
			} else {
				row.NewFile = true
				row.IsCoverageChanged = true
			}
		}
		if isSourceChanged(f.Path, upload.PathPrefix, diffFiles) {
			row.IsSourceChanged = true
		}
		row.Changed = row.IsCoverageChanged || row.IsSourceChanged
		rows = append(rows, row)
	}

	var counts fileCounts
	counts.Total = len(files)
	for _, row := range rows {
		if row.Changed {
			counts.Changed++
		}
		if row.IsSourceChanged {
			counts.Source++
		}
		if row.IsCoverageChanged {
			counts.Coverage++
		}
	}

	if base != nil {
		slices.SortStableFunc(rows, func(a, b uploadFileRow) int {
			if a.Changed != b.Changed {
				if a.Changed {
					return -1
				}
				return 1
			}
			if a.Changed {
				if c := cmp.Compare(a.DeltaVal, b.DeltaVal); c != 0 {
					return c
				}
			}
			return cmp.Compare(a.Path, b.Path)
		})
	}

	treeRows := buildFileTree(rows, base != nil)
	if heading == "" {
		heading = "Files"
	}

	return filesViewData{
		UploadID:   upload.ID,
		Files:      rows,
		TreeRows:   treeRows,
		Counts:     counts,
		HasBase:    base != nil,
		TotalFiles: len(files),
		Heading:    heading,
	}, nil
}

// uploadFileRow decorates a stored file with its coverage history for the
// upload detail table: the same file's coverage at the branch baseline, the
// resulting delta, and the lines this upload newly left uncovered. The
// baseline fields are empty when there is no baseline to compare against.
type uploadFileRow struct {
	*store.UploadFile
	Dir, Base         string     // path split for display (directory prefix, file name)
	Uncovered         string     // all-time uncovered ranges, shown when there is no baseline
	HasBefore         bool       // the file existed at the baseline
	BeforeCovered     int64      // baseline covered statements
	BeforeTotal       int64      // baseline total statements
	BeforeStr         string     // baseline coverage, preformatted
	Delta             *deltaView // after − before
	DeltaVal          float64    // after − before, for ordering
	NewFile           bool       // absent from the baseline upload
	NewlyMiss         string     // ranges covered at the baseline but uncovered now
	Changed           bool       // coverage moved, source changed, file is new, or regressed
	IsSourceChanged   bool       // changed in git diff (diff coverage)
	IsCoverageChanged bool       // coverage percentage changed or newly uncovered lines
}

// fileCounts records the totals for the view filter tabs.
type fileCounts struct {
	Total    int
	Changed  int
	Source   int
	Coverage int
}

// treeRow represents a row in the hierarchical directory tree view.
type treeRow struct {
	IsDir             bool
	Path              string
	Name              string
	Depth             int
	Open              bool // directory rendered expanded
	Hidden            bool // tucked under a collapsed ancestor
	ParentPath        string
	CoveredStmts      int64
	TotalStmts        int64
	Pct               float64
	HasBefore         bool
	BeforeCovered     int64
	BeforeTotal       int64
	BeforeStr         string
	Delta             *deltaView
	DeltaVal          float64
	NewFile           bool
	NewlyMiss         string
	Changed           bool
	IsSourceChanged   bool
	IsCoverageChanged bool
	Uncovered         string
}

type dirBuilderNode struct {
	name       string
	fullPath   string
	parentPath string
	subdirs    map[string]*dirBuilderNode
	files      []*uploadFileRow

	coveredStmts    int64
	totalStmts      int64
	beforeCovered   int64
	beforeTotal     int64
	hasBefore       bool
	changed         bool
	isSourceChanged bool
	isCovChanged    bool
}

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

func buildFileTree(rows []uploadFileRow, hasBase bool) []treeRow {
	root := &dirBuilderNode{
		subdirs: make(map[string]*dirBuilderNode),
	}

	for i := range rows {
		row := &rows[i]
		parts := strings.Split(row.Path, "/")
		if len(parts) == 1 {
			root.files = append(root.files, row)
			continue
		}
		curr := root
		var pathAcc string
		for _, p := range parts[:len(parts)-1] {
			if pathAcc == "" {
				pathAcc = p
			} else {
				pathAcc += "/" + p
			}
			child, ok := curr.subdirs[p]
			if !ok {
				child = &dirBuilderNode{
					name:       p,
					fullPath:   pathAcc,
					parentPath: curr.fullPath,
					subdirs:    make(map[string]*dirBuilderNode),
				}
				curr.subdirs[p] = child
			}
			curr = child
		}
		curr.files = append(curr.files, row)
	}

	compactDir(root)
	calcDirStats(root)

	var result []treeRow
	anyChanged := slices.ContainsFunc(rows, func(r uploadFileRow) bool { return r.Changed })
	flattenTree(root, 0, &result, hasBase, anyChanged, false)
	return result
}

func compactDir(n *dirBuilderNode) {
	for _, sub := range n.subdirs {
		compactDir(sub)
	}
	if n.fullPath != "" && len(n.files) == 0 && len(n.subdirs) == 1 {
		var child *dirBuilderNode
		for _, c := range n.subdirs {
			child = c
		}
		n.name = n.name + "/" + child.name
		n.fullPath = child.fullPath
		n.files = child.files
		n.subdirs = child.subdirs
		for _, grand := range n.subdirs {
			grand.parentPath = n.fullPath
		}
	}
}

func calcDirStats(n *dirBuilderNode) {
	for _, f := range n.files {
		n.totalStmts += f.TotalStmts
		n.coveredStmts += f.CoveredStmts
		if f.HasBefore {
			n.hasBefore = true
			n.beforeCovered += f.BeforeCovered
			n.beforeTotal += f.BeforeTotal
		}
		if f.Changed {
			n.changed = true
		}
		if f.IsSourceChanged {
			n.isSourceChanged = true
		}
		if f.IsCoverageChanged {
			n.isCovChanged = true
		}
	}
	for _, sub := range n.subdirs {
		calcDirStats(sub)
		n.totalStmts += sub.totalStmts
		n.coveredStmts += sub.coveredStmts
		if sub.hasBefore {
			n.hasBefore = true
			n.beforeCovered += sub.beforeCovered
			n.beforeTotal += sub.beforeTotal
		}
		if sub.changed {
			n.changed = true
		}
		if sub.isSourceChanged {
			n.isSourceChanged = true
		}
		if sub.isCovChanged {
			n.isCovChanged = true
		}
	}
}

// flattenTree emits the tree in display order. Directories start collapsed
// except the ones on the way to a changed file, so a large repo opens as a
// short list of top-level folders with the commit's changes already in view;
// when nothing changed (or there is no baseline to change against) the
// top level is open instead. hidden is inherited from a collapsed ancestor.
func flattenTree(n *dirBuilderNode, depth int, out *[]treeRow, hasBase, anyChanged, hidden bool) {
	if n.fullPath != "" {
		open := n.changed || (!anyChanged && depth == 0)
		var pct float64
		if n.totalStmts > 0 {
			pct = float64(n.coveredStmts) / float64(n.totalStmts) * 100
		}
		var beforeStr string
		var delta *deltaView
		var deltaVal float64
		if hasBase && n.hasBefore && n.beforeTotal > 0 {
			beforePct := float64(n.beforeCovered) / float64(n.beforeTotal) * 100
			deltaVal = pct - beforePct
			delta = newDeltaView(deltaVal)
			beforeStr = fmt.Sprintf("%.1f%%", beforePct)
		}
		row := treeRow{
			IsDir:             true,
			Path:              n.fullPath,
			Name:              n.name,
			Depth:             depth,
			Open:              open,
			Hidden:            hidden,
			ParentPath:        n.parentPath,
			CoveredStmts:      n.coveredStmts,
			TotalStmts:        n.totalStmts,
			Pct:               pct,
			HasBefore:         n.hasBefore && n.beforeTotal > 0,
			BeforeCovered:     n.beforeCovered,
			BeforeTotal:       n.beforeTotal,
			BeforeStr:         beforeStr,
			Delta:             delta,
			DeltaVal:          deltaVal,
			Changed:           n.changed,
			IsSourceChanged:   n.isSourceChanged,
			IsCoverageChanged: n.isCovChanged,
		}
		*out = append(*out, row)
		depth++
		hidden = hidden || !open
	}

	for _, name := range slices.Sorted(maps.Keys(n.subdirs)) {
		flattenTree(n.subdirs[name], depth, out, hasBase, anyChanged, hidden)
	}

	slices.SortFunc(n.files, func(a, b *uploadFileRow) int { return strings.Compare(a.Base, b.Base) })
	for _, f := range n.files {
		row := treeRow{
			IsDir:             false,
			Path:              f.Path,
			Name:              f.Base,
			Depth:             depth,
			Hidden:            hidden,
			ParentPath:        n.fullPath,
			CoveredStmts:      f.CoveredStmts,
			TotalStmts:        f.TotalStmts,
			Pct:               f.Pct,
			HasBefore:         f.HasBefore,
			BeforeCovered:     f.BeforeCovered,
			BeforeTotal:       f.BeforeTotal,
			BeforeStr:         f.BeforeStr,
			Delta:             f.Delta,
			DeltaVal:          f.DeltaVal,
			NewFile:           f.NewFile,
			NewlyMiss:         f.NewlyMiss,
			Changed:           f.Changed,
			IsSourceChanged:   f.IsSourceChanged,
			IsCoverageChanged: f.IsCoverageChanged,
			Uncovered:         f.Uncovered,
		}
		*out = append(*out, row)
	}
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
	Ignored      string // "3 files ignored", "" when no pattern matched
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
	slices.SortFunc(spans, func(a, b span) int { return cmp.Compare(a.start, b.start) })
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
	slices.Sort(lines)
	return diffcov.Ranges(lines)
}

// splitPath separates a file path into its directory prefix (with trailing
// slash) and file name, so the table can dim the directory.
func splitPath(p string) (dir, base string) {
	if dir, base, ok := strings.CutLast(p, "/"); ok {
		return dir + "/", base
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
	if _, ok := s.authorizeReport(w, r, repo); !ok {
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
