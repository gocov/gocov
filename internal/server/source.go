package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// maxSourceBytes bounds source files rendered by the source view.
const maxSourceBytes = 1 << 20

// sourceLine is one rendered line of the source view.
type sourceLine struct {
	No      int
	Class   string // "hit", "miss" or "" for non-executable lines
	Count   int    // executions, meaningful only on an executable line
	Text    string
	NewMiss bool // uncovered now but covered at the baseline commit
}

// handleSource implements GET /uploads/{id}/files/{path...} — the file's
// source at the upload's commit with per-line coverage overlay. The
// upload decides access, exactly as on the upload page; which paths the
// upload recorded is the UI API's answer to give, so the page neither
// reads the file list nor fetches the source.
func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.reportUpload(w, r); !ok {
		return
	}
	s.serveApp(w, r, http.StatusOK, sourcePageHead(r.PathValue("path")))
}

// sourcePageData is one file's source at an upload's commit with its
// coverage overlay, as read from the store and the forge. The UI API
// hands the lines over as they are; the folds and the miss rail are the
// client's to draw.
type sourcePageData struct {
	Repo   *store.Repo
	Upload *store.Upload
	File   *store.UploadFile
	// Unavailable is why no source could be shown; Lines is then empty.
	Unavailable string
	Lines       []sourceLine
	// Delta is the file's coverage against the baseline commit, nil when
	// there is no baseline (or no source to compare line by line).
	Delta *float64
}

// buildSourcePage assembles the source view. A false second result means
// the answer is already written.
func (s *Server) buildSourcePage(w http.ResponseWriter, r *http.Request) (*sourcePageData, bool) {
	upload, repo, ok := s.reportUpload(w, r)
	if !ok {
		return nil, false
	}
	path := r.PathValue("path")
	// Access was settled before this per-file lookup, so a signed-out
	// probe cannot tell a missing file from a missing upload.
	files, err := s.store.UploadFiles(r.Context(), upload.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, "loading upload files", err)
		return nil, false
	}
	i := slices.IndexFunc(files, func(f *store.UploadFile) bool { return f.Path == path })
	if i < 0 {
		httpError(w, http.StatusNotFound, "not found")
		return nil, false
	}
	file := files[i]

	source, unavailable := s.fetchSource(r, repo, upload, file)
	d := &sourcePageData{Repo: repo, Upload: upload, File: file, Unavailable: unavailable}
	if unavailable == "" {
		d.Lines = renderSourceLines(source, file.Blocks)
		// Compare against the file at the previous baseline commit to flag
		// regressions and show a coverage delta.
		if base := s.baseFileFor(r.Context(), repo, upload, file.Path); base != nil {
			markNewlyUncovered(d.Lines, base.Blocks)
			d.Delta = new(file.Pct - base.Pct)
		}
	}
	return d, true
}

// sourcePageDTO is the source view for the app: the lines with their
// coverage, and nothing computed from them — the folds and the miss rail
// are the client's to draw.
type sourcePageDTO struct {
	Repo   repoRefDTO      `json:"repo"`
	Upload sourceUploadDTO `json:"upload"`
	File   sourceFileDTO   `json:"file"`
	Delta  *float64        `json:"delta"`
	// Unavailable is non-empty when the source could not be fetched, and
	// says why; Lines is then empty.
	Unavailable string          `json:"unavailable"`
	Uncovered   string          `json:"uncovered"`
	Lines       []sourceLineDTO `json:"lines"`
}

type sourceUploadDTO struct {
	ID  int64  `json:"id"`
	SHA string `json:"sha"`
}

type sourceFileDTO struct {
	Path         string  `json:"path"`
	Coverage     float64 `json:"coverage"`
	CoveredStmts int64   `json:"covered_stmts"`
	TotalStmts   int64   `json:"total_stmts"`
}

// sourceLineDTO is one line of source: its text, how often it ran (null
// when it is not a statement) and whether this commit newly lost it.
type sourceLineDTO struct {
	No      int    `json:"no"`
	Text    string `json:"text"`
	Hits    *int   `json:"hits"`
	NewMiss bool   `json:"new_miss"`
}

// handleAPISource implements GET /api/ui/uploads/{id}/files/{path...}.
func (s *Server) handleAPISource(w http.ResponseWriter, r *http.Request) {
	d, ok := s.buildSourcePage(w, r)
	if !ok {
		return
	}
	dto := sourcePageDTO{
		Repo:   newRepoRefDTO(d.Repo),
		Upload: sourceUploadDTO{ID: d.Upload.ID, SHA: d.Upload.CommitSHA},
		File: sourceFileDTO{
			Path:         d.File.Path,
			Coverage:     d.File.Pct,
			CoveredStmts: d.File.CoveredStmts,
			TotalStmts:   d.File.TotalStmts,
		},
		Delta:       d.Delta,
		Unavailable: d.Unavailable,
		Uncovered:   uncoveredRanges(d.File.Blocks),
		Lines:       make([]sourceLineDTO, 0, len(d.Lines)),
	}
	for _, line := range d.Lines {
		l := sourceLineDTO{No: line.No, Text: line.Text, NewMiss: line.NewMiss}
		if line.Class != "" { // executable: a statement block spans it
			l.Hits = new(line.Count)
		}
		dto.Lines = append(dto.Lines, l)
	}
	s.writeJSON(w, dto)
}

// fetchSource returns the file content at the upload's commit, preferring
// the blobstore cache — commit content is immutable, so a cached copy
// never goes stale and keeps forge API usage down. On any failure it
// returns a human-readable reason instead; the page then falls back to
// the uncovered-ranges summary.
func (s *Server) fetchSource(r *http.Request, repo *store.Repo, u *store.Upload, f *store.UploadFile) ([]byte, string) {
	// Profile paths may be module-qualified; the forge wants repo paths.
	repoPath := f.Path
	if u.PathPrefix != "" {
		repoPath = strings.TrimPrefix(f.Path, u.PathPrefix+"/")
	}
	// Profile content is attacker-influencable by any token holder; a
	// path with dot segments could normalize into a different forge API
	// endpoint fetched with the bot's credentials.
	if !safeRepoPath(repoPath) {
		return nil, "the recorded file path cannot be requested from the forge"
	}

	cacheKey := fmt.Sprintf("source/%d/%s/%s", repo.ID, u.CommitSHA, repoPath)
	if cached, err := s.blobs.Get(r.Context(), cacheKey); err == nil {
		return s.validateSource(cached)
	}
	// A commit's tree is immutable, so "not found" verdicts are cached
	// too — otherwise every view of an unresolvable file would replay
	// the whole probe sequence against the forge API, and this page
	// may be reachable without sign-in (auth is opt-in).
	missKey := fmt.Sprintf("source-miss/%d/%s/%s", repo.ID, u.CommitSHA, repoPath)
	notFound := fmt.Sprintf("%s was not found at commit %s on %s", repoPath, u.CommitSHA, repo.Forge)
	if _, err := s.blobs.Get(r.Context(), missKey); err == nil {
		return nil, notFound
	}

	fg, err := s.forges.For(r.Context(), repo)
	if err != nil {
		return nil, "no working forge integration: " + err.Error()
	}
	if fg == nil {
		return nil, "this repo's workspace is not connected to its forge"
	}

	// Probing with trimmed prefixes exists for uploads whose stored
	// path_prefix could not map the recorded path to a repo path. When a
	// prefix was applied, the result is authoritative: a 404 then means
	// the file genuinely is not at that commit, and probing could only
	// ever surface a wrong same-suffix file.
	candidates := []string{repoPath}
	if u.PathPrefix == "" {
		candidates = sourceCandidates(repoPath)
	}
	var content []byte
	found := false
	var fetchErr error
	for _, cand := range candidates {
		b, err := fg.GetFileContent(r.Context(), repo.Slug, u.CommitSHA, cand)
		if err != nil {
			fetchErr = err
			if errors.Is(err, forge.ErrRepoNotFound) {
				continue
			}
			break
		}
		// A trimmed candidate is a guess; reject it when the file is
		// shorter than the lines the profile claims to cover — that is
		// a same-suffix collision with an unrelated file, and rendering
		// it would overlay meaningless coverage.
		if cand != repoPath && countLines(b) < maxBlockLine(f.Blocks) {
			s.log.Info("trimmed source candidate rejected as too short",
				"repo", repo.Slug, "recorded", repoPath, "candidate", cand)
			fetchErr = forge.ErrRepoNotFound
			continue
		}
		if cand != repoPath {
			s.log.Info("source resolved via trimmed path",
				"repo", repo.Slug, "recorded", repoPath, "resolved", cand)
		}
		content, found = b, true
		break
	}
	if !found {
		if fetchErr == nil || errors.Is(fetchErr, forge.ErrRepoNotFound) {
			if err := s.blobs.Put(r.Context(), missKey, []byte{'-'}); err != nil {
				s.log.Warn("cache source miss", "key", missKey, "err", err)
			}
			return nil, notFound
		}
		if errors.Is(fetchErr, forge.ErrNotImplemented) {
			return nil, "this forge does not support reading files"
		}
		// Forge error text can carry API URLs and response bodies; log
		// it but keep the page generic.
		s.log.Warn("fetch source", "repo", repo.Slug, "path", repoPath, "err", fetchErr)
		return nil, "fetching the file from the forge failed"
	}
	content, reason := s.validateSource(content)
	if reason != "" {
		return nil, reason
	}
	if err := s.blobs.Put(r.Context(), cacheKey, content); err != nil {
		s.log.Warn("cache source", "key", cacheKey, "err", err)
	}
	return content, ""
}

// maxSourceProbes bounds forge API calls per source-view render: the
// recorded path plus a handful of trimmed variants.
const maxSourceProbes = 8

// sourceCandidates lists the repo paths to ask the forge for, in order.
// Profiles frequently record paths with extra leading directories that
// the stored path_prefix did not cover — a Go module path on uploads
// made before prefixes were stored, or a CI checkout directory in a
// Cobertura report. Trimming leading segments one at a time finds the
// repo path without any configuration. Trimmed candidates keep at
// least two segments so a bare filename cannot silently match an
// unrelated file at the repo root; every candidate is a suffix of the
// already-validated path, so it stays safe to request. When there are
// more suffixes than the probe budget, both ends are kept: short trims
// resolve module-qualified paths, deep trims resolve CI checkout
// prefixes — the middle is the least likely to be a repo root.
func sourceCandidates(repoPath string) []string {
	segs := strings.Split(repoPath, "/")
	var suffixes []string
	for i := 1; i <= len(segs)-2; i++ {
		suffixes = append(suffixes, strings.Join(segs[i:], "/"))
	}
	if len(suffixes) > maxSourceProbes-1 {
		head := suffixes[:(maxSourceProbes-1)/2]
		tail := suffixes[len(suffixes)-(maxSourceProbes-1-len(head)):]
		suffixes = append(head, tail...)
	}
	return append([]string{repoPath}, suffixes...)
}

// countLines reports how many lines content has, counting a trailing
// partial line.
func countLines(b []byte) int {
	n := bytes.Count(b, []byte{'\n'})
	if len(b) > 0 && b[len(b)-1] != '\n' {
		n++
	}
	return n
}

// maxBlockLine is the highest line the profile claims for the file.
func maxBlockLine(blocks []profile.Block) int {
	last := 0
	for _, b := range blocks {
		last = max(last, b.EndLine)
	}
	return last
}

// safeRepoPath accepts only plain relative paths: no empty, "." or ".."
// segments and no leading slash.
func safeRepoPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") {
		return false
	}
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func (s *Server) validateSource(content []byte) ([]byte, string) {
	if len(content) > maxSourceBytes {
		return nil, "the file is too large to display"
	}
	if !utf8.Valid(content) {
		return nil, "the file is not valid UTF-8 text"
	}
	return content, ""
}

// renderSourceLines overlays coverage blocks onto source lines. A line is
// executable when any block spans it; it is covered when any such block
// has a positive count — the same rule diff coverage uses.
func renderSourceLines(source []byte, blocks []profile.Block) []sourceLine {
	text := strings.TrimSuffix(string(source), "\n")
	rawLines := strings.Split(text, "\n")
	counts := lineCounts(blocks, len(rawLines))

	lines := make([]sourceLine, 0, len(rawLines))
	for i, raw := range rawLines {
		no := i + 1
		line := sourceLine{No: no, Text: strings.TrimSuffix(raw, "\r")}
		if n, executable := counts[no]; executable {
			line.Count = n
			line.Class = "miss"
			if n > 0 {
				line.Class = "hit"
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// baseBaselineScan bounds how far back the baseline search reads uploads.
const baseBaselineScan = 60

// baselineUpload returns the upload this one should be compared against — the
// newest earlier non-PR, gate-passing upload on the same branch, falling back
// to the default branch's latest passing upload for PR and feature-branch
// builds (which usually have no prior upload of their own). This mirrors the
// gate/delta baseline in recomputeCommitReport, so the page's "before → after"
// and BASE agree with the commit's delta. Returns (nil, nil) when there is
// nothing to compare against. The per-file coverage is keyed by profile path.
func (s *Server) baselineUpload(ctx context.Context, repo *store.Repo, u *store.Upload) (*store.Upload, map[string]*store.UploadFile) {
	// On the upload's own branch: the newest strictly-earlier passing,
	// non-PR upload.
	base := s.pickBaseline(ctx, repo.ID, u.Branch, func(prev *store.Upload) bool {
		return prev.ID < u.ID && prev.PRID == "" && !prev.GateFailed
	})
	// PR and feature branches rarely have such an upload of their own; fall
	// back to the default branch's latest passing upload — the state this
	// commit would merge into.
	if base == nil && u.Branch != repo.DefaultBranch {
		base = s.pickBaseline(ctx, repo.ID, repo.DefaultBranch, func(prev *store.Upload) bool {
			return prev.CommitSHA != u.CommitSHA && prev.PRID == "" && !prev.GateFailed
		})
	}
	if base == nil {
		return nil, nil
	}
	files, err := s.store.UploadFiles(ctx, base.ID)
	if err != nil {
		return nil, nil
	}
	byPath := make(map[string]*store.UploadFile, len(files))
	for _, f := range files {
		byPath[f.Path] = f
	}
	return base, byPath
}

// pickBaseline returns the newest upload on a branch (uploads come newest
// first) that satisfies ok, or nil. Bounded by baseBaselineScan.
func (s *Server) pickBaseline(ctx context.Context, repoID int64, branch string, ok func(*store.Upload) bool) *store.Upload {
	ups, err := s.store.ListBranchUploads(ctx, repoID, branch, baseBaselineScan)
	if err != nil {
		return nil
	}
	if i := slices.IndexFunc(ups, ok); i >= 0 {
		return ups[i]
	}
	return nil
}

// baseFileFor returns the same file at the most recent prior baseline upload
// on the upload's branch. A file absent from that upload is genuinely new, so
// there is no baseline to compare against.
func (s *Server) baseFileFor(ctx context.Context, repo *store.Repo, u *store.Upload, path string) *store.UploadFile {
	_, byPath := s.baselineUpload(ctx, repo, u)
	return byPath[path]
}

// markNewlyUncovered flags each line that is uncovered now but was covered
// at the baseline, and returns how many. Matching is by line number, so it
// surfaces regressions on a best-effort basis without a full diff.
func markNewlyUncovered(lines []sourceLine, baseBlocks []profile.Block) int {
	baseCounts := lineCounts(baseBlocks, len(lines))
	n := 0
	for i := range lines {
		if lines[i].Class == "miss" && baseCounts[lines[i].No] > 0 {
			lines[i].NewMiss = true
			n++
		}
	}
	return n
}
