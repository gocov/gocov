package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
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
	Hits    string // "3×", "✗" or ""
	Text    string
	Anchor  string // element id on the first line of a miss run, else ""
	NewMiss bool   // uncovered now but covered at the baseline commit
	FoldID  string // set when the line sits inside a collapsed fold
}

// sourceItem is one row of the rendered source: either a code line or a
// fold bar standing in for a collapsed run of covered lines.
type sourceItem struct {
	Line *sourceLine
	Fold *foldInfo
}

// foldInfo is a collapsed run of contiguous non-miss lines.
type foldInfo struct {
	ID    string
	Lines int
	Label string
}

// missBlock is one contiguous run of uncovered lines, positioned against
// the file height for the source view's miss-map rail.
type missBlock struct {
	Anchor    string // id of the run's first line, e.g. "L88"
	StartLine int
	EndLine   int
	Lines     int     // uncovered lines in the run
	Top       float64 // rail offset, percent of file height
	Height    float64 // rail length, percent of file height
}

// handleSource implements GET /uploads/{id}/files/{path...} — the file's
// source at the upload's commit with per-line coverage overlay. Only paths
// recorded in the upload can be viewed.
func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	path := r.PathValue("path")
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
	var file *store.UploadFile
	for _, f := range files {
		if f.Path == path {
			file = f
			break
		}
	}
	if file == nil {
		http.NotFound(w, r)
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

	source, unavailable := s.fetchSource(r, repo, upload, file)
	dir, base := file.Path, ""
	if i := strings.LastIndex(file.Path, "/"); i >= 0 {
		dir, base = file.Path[:i+1], file.Path[i+1:]
	} else {
		dir, base = "", file.Path
	}
	data := map[string]any{
		"Repo":           repo,
		"Upload":         upload,
		"File":           file,
		"FileDir":        dir,
		"FileBase":       base,
		"Uncovered":      uncoveredRanges(file.Blocks),
		"Unavailable":    unavailable, // reason string when no source could be shown
		"Lines":          nil,
		"Items":          nil,
		"MissBlocks":     nil,
		"MissLines":      0,
		"NewlyUncovered": 0,
		"Delta":          nil,
	}
	if unavailable == "" {
		lines := renderSourceLines(source, file.Blocks)
		blocks, missLines := annotateMisses(lines)
		// Compare against the file at the previous baseline commit to flag
		// regressions and show a coverage delta.
		if base := s.baseFileFor(r.Context(), repo, upload, file.Path); base != nil {
			data["NewlyUncovered"] = markNewlyUncovered(lines, base.Blocks)
			d := file.Pct - base.Pct
			cls, arrow := "flat", "→"
			switch {
			case d > 0.05:
				cls, arrow = "up", "▲"
			case d < -0.05:
				cls, arrow = "down", "▼"
			}
			data["Delta"] = map[string]any{"Class": cls, "Arrow": arrow, "Text": fmt.Sprintf("%+.1f%%", d)}
		}
		data["Lines"] = lines
		data["Items"] = foldItems(lines)
		data["MissBlocks"] = blocks
		data["MissLines"] = missLines
	}
	s.render(w, r, "source.html", data)
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

	fg, err := s.forgeFor(r.Context(), repo)
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
	max := 0
	for _, b := range blocks {
		if b.EndLine > max {
			max = b.EndLine
		}
	}
	return max
}

// safeRepoPath accepts only plain relative paths: no empty, "." or ".."
// segments and no leading slash.
func safeRepoPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
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

	covered := map[int]bool{}
	counts := map[int]int{}
	for _, b := range blocks {
		// Parsers validate line ranges, but old rows predate that; never
		// let a bogus range drive a long loop.
		for l := max(b.StartLine, 1); l <= b.EndLine && l <= len(rawLines); l++ {
			covered[l] = covered[l] || b.Count > 0
			if b.Count > counts[l] {
				counts[l] = b.Count
			}
		}
	}

	lines := make([]sourceLine, 0, len(rawLines))
	for i, raw := range rawLines {
		no := i + 1
		line := sourceLine{No: no, Text: strings.TrimSuffix(raw, "\r")}
		if hit, executable := covered[no]; executable {
			if hit {
				line.Class = "hit"
				line.Hits = fmt.Sprintf("%d×", counts[no])
			} else {
				line.Class = "miss"
				line.Hits = "✗"
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// minMissHeight keeps a one- or two-line miss run tall enough to stay a
// clickable target on the rail even in a long file.
const minMissHeight = 0.8

// annotateMisses tags the first line of each contiguous uncovered run with
// a jump anchor and returns those runs positioned against the file height
// for the miss-map rail, plus the total uncovered-line count. It mutates
// lines to set the anchors.
func annotateMisses(lines []sourceLine) ([]missBlock, int) {
	total := len(lines)
	var blocks []missBlock
	missLines := 0
	for i := 0; i < len(lines); {
		if lines[i].Class != "miss" {
			i++
			continue
		}
		j := i
		for j < len(lines) && lines[j].Class == "miss" {
			j++
		}
		run := j - i
		missLines += run
		anchor := fmt.Sprintf("L%d", lines[i].No)
		lines[i].Anchor = anchor
		var top, height float64
		if total > 0 {
			top = float64(lines[i].No-1) / float64(total) * 100
			height = float64(run) / float64(total) * 100
		}
		if height < minMissHeight {
			height = minMissHeight
		}
		blocks = append(blocks, missBlock{
			Anchor: anchor, StartLine: lines[i].No, EndLine: lines[j-1].No,
			Lines: run, Top: top, Height: height,
		})
		i = j
	}
	return blocks, missLines
}

// baseBaselineScan bounds how far back the baseline search reads uploads.
const baseBaselineScan = 60

// baseFileFor returns the same file at the most recent prior baseline
// upload on the upload's branch — the newest earlier upload that is not a
// PR build and did not fail the gate. A file absent from that upload is
// genuinely new, so there is no baseline to compare against.
func (s *Server) baseFileFor(ctx context.Context, repo *store.Repo, u *store.Upload, path string) *store.UploadFile {
	ups, err := s.store.ListBranchUploads(ctx, repo.ID, u.Branch, baseBaselineScan)
	if err != nil {
		return nil
	}
	var base *store.Upload
	for _, prev := range ups {
		if prev.ID >= u.ID || prev.PRID != "" || prev.GateFailed {
			continue
		}
		base = prev
		break
	}
	if base == nil {
		return nil
	}
	files, err := s.store.UploadFiles(ctx, base.ID)
	if err != nil {
		return nil
	}
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	return nil
}

// markNewlyUncovered flags each line that is uncovered now but was covered
// at the baseline, and returns how many. Matching is by line number, so it
// surfaces regressions on a best-effort basis without a full diff.
func markNewlyUncovered(lines []sourceLine, baseBlocks []profile.Block) int {
	baseHit := map[int]bool{}
	for _, b := range baseBlocks {
		if b.Count <= 0 {
			continue
		}
		for l := max(b.StartLine, 1); l <= b.EndLine; l++ {
			baseHit[l] = true
		}
	}
	n := 0
	for i := range lines {
		if lines[i].Class == "miss" && baseHit[lines[i].No] {
			lines[i].NewMiss = true
			n++
		}
	}
	return n
}

// foldThreshold is the shortest run of contiguous covered/non-executable
// lines that collapses into a fold bar. Uncovered lines never fold.
const foldThreshold = 10

// foldItems lays the rendered lines out as display rows, collapsing long
// runs of non-miss lines into fold bars. Each folded line keeps a FoldID
// so the client can reveal exactly that run when its bar is expanded.
func foldItems(lines []sourceLine) []sourceItem {
	items := make([]sourceItem, 0, len(lines))
	folds := 0
	for i := 0; i < len(lines); {
		if lines[i].Class == "miss" {
			ln := lines[i]
			items = append(items, sourceItem{Line: &ln})
			i++
			continue
		}
		j := i
		hasHit := false
		for j < len(lines) && lines[j].Class != "miss" {
			hasHit = hasHit || lines[j].Class == "hit"
			j++
		}
		run := j - i
		if run >= foldThreshold {
			folds++
			id := fmt.Sprintf("f%d", folds)
			label := fmt.Sprintf("%d lines", run)
			if hasHit {
				label += ", fully covered"
			}
			items = append(items, sourceItem{Fold: &foldInfo{ID: id, Lines: run, Label: label}})
			for k := i; k < j; k++ {
				ln := lines[k]
				ln.FoldID = id
				items = append(items, sourceItem{Line: &ln})
			}
		} else {
			for k := i; k < j; k++ {
				ln := lines[k]
				items = append(items, sourceItem{Line: &ln})
			}
		}
		i = j
	}
	return items
}
