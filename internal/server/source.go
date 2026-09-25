package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// maxSourceBytes bounds source files rendered by the source view. The
// forge clients fetch up to forge.MaxFileBytes, above this, so a file just
// past it is refused with a reason rather than a failed fetch.
const maxSourceBytes = 1 << 20

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

// buildSourcePage assembles the source view for the app: one file's source
// at an upload's commit with its coverage overlay, read from the store and
// the forge. The lines go over as they are; the folds and the miss rail are
// the client's to draw. A false second result means the answer is already
// written.
func (s *Server) buildSourcePage(w http.ResponseWriter, r *http.Request) (*sourcePageDTO, bool) {
	upload, repo, ok := s.reportUpload(w, r)
	if !ok {
		return nil, false
	}
	path := r.PathValue("path")
	// Access was settled before this per-file lookup, so a signed-out
	// probe cannot tell a missing file from a missing upload.
	file, err := s.store.UploadFile(r.Context(), upload.ID, path)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "not found")
		return nil, false
	}
	if err != nil {
		s.internalError(w, "loading upload file", err)
		return nil, false
	}

	// The source (a forge round trip on a cache miss) and the file at the
	// baseline commit are independent reads.
	var (
		wg          sync.WaitGroup
		source      []byte
		unavailable string
		base        *store.UploadFile
	)
	wg.Go(func() { source, unavailable = s.fetchSource(r, repo, upload, file) })
	wg.Go(func() { base = s.baseFileFor(r.Context(), repo, upload, file.Path) })
	wg.Wait()
	dto := &sourcePageDTO{
		Repo:   newRepoRefDTO(repo),
		Upload: sourceUploadDTO{ID: upload.ID, SHA: upload.CommitSHA},
		File: sourceFileDTO{
			Path:         file.Path,
			Coverage:     file.Pct,
			CoveredStmts: file.CoveredStmts,
			TotalStmts:   file.TotalStmts,
		},
		Unavailable: unavailable,
		Uncovered:   uncoveredRanges(file.Blocks),
		Lines:       []sourceLineDTO{},
	}
	if unavailable == "" {
		dto.Lines = renderSourceLines(source, file.Blocks)
		// Compare against the file at the previous baseline commit to flag
		// regressions and show a coverage delta.
		if base != nil {
			markNewlyUncovered(dto.Lines, base.Blocks)
			dto.Delta = new(file.Pct - base.Pct)
		}
	}
	return dto, true
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
	NewMiss bool   `json:"new_miss"` // uncovered now but covered at the baseline commit
}

// missed reports an executable line that did not run.
func (l sourceLineDTO) missed() bool { return l.Hits != nil && *l.Hits == 0 }

// handleAPISource implements GET /api/ui/uploads/{id}/files/{path...}.
func (s *Server) handleAPISource(w http.ResponseWriter, r *http.Request) {
	if dto, ok := s.buildSourcePage(w, r); ok {
		s.writeJSON(w, dto)
	}
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

	fg := s.forges.For(r.Context(), repo)
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
func renderSourceLines(source []byte, blocks []profile.Block) []sourceLineDTO {
	text := strings.TrimSuffix(string(source), "\n")
	rawLines := strings.Split(text, "\n")
	counts := lineCounts(blocks, len(rawLines))

	lines := make([]sourceLineDTO, 0, len(rawLines))
	for i, raw := range rawLines {
		no := i + 1
		line := sourceLineDTO{No: no, Text: strings.TrimSuffix(raw, "\r")}
		if n, executable := counts[no]; executable {
			line.Hits = new(n)
		}
		lines = append(lines, line)
	}
	return lines
}

// baselineUpload returns the upload this one should be compared against
// (core.UploadBaseline) with its per-file coverage keyed by profile path,
// or (nil, nil) when there is nothing to compare against.
func (s *Server) baselineUpload(ctx context.Context, repo *store.Repo, u *store.Upload) (*store.Upload, map[string]*store.UploadFile) {
	base := core.UploadBaseline(ctx, s.store, repo, u)
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

// baseFileFor returns the same file at the baseline upload
// (core.UploadBaseline), reading only that one file. A file absent from
// that upload is genuinely new, so there is no baseline to compare against.
func (s *Server) baseFileFor(ctx context.Context, repo *store.Repo, u *store.Upload, path string) *store.UploadFile {
	base := core.UploadBaseline(ctx, s.store, repo, u)
	if base == nil {
		return nil
	}
	f, err := s.store.UploadFile(ctx, base.ID, path)
	if err != nil {
		return nil
	}
	return f
}

// markNewlyUncovered flags each line that is uncovered now but was covered
// at the baseline, and returns how many. Matching is by line number, so it
// surfaces regressions on a best-effort basis without a full diff.
func markNewlyUncovered(lines []sourceLineDTO, baseBlocks []profile.Block) int {
	baseCounts := lineCounts(baseBlocks, len(lines))
	n := 0
	for i := range lines {
		if lines[i].missed() && baseCounts[lines[i].No] > 0 {
			lines[i].NewMiss = true
			n++
		}
	}
	return n
}
