// The upload endpoint: the one write path into gocov. A CI job posts a
// coverage profile here, and this file runs it end to end — authenticate,
// read and validate the request, parse the profile, store it, merge the
// commit's parts, then report back to the uploader. The steps it delegates
// live next door: gate.go, merge.go, forgepush.go and uploadrepo.go.

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// maxUploadBytes bounds the whole multipart request body.
const maxUploadBytes = 64 << 20

// maxPartsPerCommit caps how many distinct parts one commit's merged report
// is built from. It bounds recompute cost (every upload re-reads and
// re-merges every part under the lock) and catches the -part $CI_JOB_ID
// mistake, where a varying part name accumulates parts without end. Well
// above any real matrix.
const maxPartsPerCommit = 50

type uploadResponse struct {
	ID           int64    `json:"id"`
	TotalPct     float64  `json:"total_pct"`
	CoveredStmts int64    `json:"covered_stmts"`
	TotalStmts   int64    `json:"total_stmts"`
	DeltaPct     *float64 `json:"delta_pct,omitempty"`
	BuildStatus  string   `json:"build_status"`  // "posted", "skipped" or "error: ..."
	CodeInsights string   `json:"code_insights"` // "posted", "skipped" or "error: ..."
	// RepoCreated reports that this upload auto-registered the repo
	// through a workspace token.
	RepoCreated bool `json:"repo_created,omitempty"`

	// Gate reports the coverage-gate outcome: "passed" or
	// "failed: <reasons>". Omitted when the repo has no gate configured.
	Gate string `json:"gate,omitempty"`

	// Warnings carries non-fatal notices about how the merged report was
	// built — e.g. a diff-coverage file merged conservatively because its
	// parts disagreed. Omitted when there are none.
	Warnings []string `json:"warnings,omitempty"`

	// PR-only fields, set when pr_id was part of the upload.
	DiffPct          *float64 `json:"diff_pct,omitempty"`
	DiffCoveredLines *int64   `json:"diff_covered_lines,omitempty"`
	DiffTotalLines   *int64   `json:"diff_total_lines,omitempty"`
	DiffStatus       string   `json:"diff_status,omitempty"` // "computed", "skipped: ..." or "error: ..."
	PRComment        string   `json:"pr_comment,omitempty"`  // "posted", "updated", "skipped" or "error: ..."
}

// knownCIProviders bounds the ci_provider field to the forges gocov knows,
// so the value is safe to key display labels off.
var knownCIProviders = map[string]bool{"github": true, "gitlab": true, "bitbucket": true}

// buildUploadMeta assembles the upload's provenance from the request. The CLI
// sends the source fields (uploader, CI run, commit subject and author) from
// the CI environment; the server measures the profile size and its own
// processing time. Every field is optional and bounded — the values are
// attacker-influencable by any token holder and end up rendered on the
// upload page.
func buildUploadMeta(r *http.Request, filename string, rawLen int, elapsed time.Duration) store.UploadMeta {
	m := store.UploadMeta{
		Uploader:      clip(r.FormValue("uploader"), 60),
		CommitMessage: clip(firstLine(r.FormValue("commit_message")), 200),
		CommitAuthor:  clip(r.FormValue("commit_author"), 120),
		ProfileName:   clip(baseName(filename), 200),
		ProfileBytes:  int64(rawLen),
		ProcessMillis: elapsed.Milliseconds(),
	}
	if kind := r.FormValue("uploader_kind"); kind == "cli" || kind == "action" {
		m.UploaderKind = kind
	}
	if p := strings.ToLower(r.FormValue("ci_provider")); knownCIProviders[p] {
		m.CIProvider = p
	}
	// Only keep an http(s) run URL; anything else could smuggle a
	// javascript: or data: link into the page's href.
	if raw := clip(r.FormValue("ci_run_url"), 500); raw != "" {
		if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			m.CIRunURL = raw
		}
	}
	return m
}

// clip trims surrounding space and caps a string to n runes.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n]))
	}
	return s
}

// firstLine returns the first line of s, so a multi-line commit message
// contributes only its subject.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// baseName strips any directory part a client put on the profile filename.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// handleUpload implements POST /api/v1/upload.
//
// Auth: Bearer token — either a per-repo token or a workspace token.
// With a workspace token the repo field is required; unknown repos under
// the workspace prefix are registered automatically. Multipart form: file
// field "profile"; value fields repo, commit (required), branch (defaults
// to the repo's default branch), pr_id (optional), format (default "go").
//
// The body below is the flow and nothing else — authenticate, read the
// request, store what arrived, merge the commit's parts, answer the
// uploader, tell the forge — with each step's detail one call away.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		httpError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	// Authenticate before touching the body so invalid tokens cost a
	// lookup, not a 64MB multipart parse.
	authedRepo, ws, ok := s.lookupUploadToken(w, r, token)
	if !ok {
		return
	}
	req, ok := s.readUploadRequest(w, r, authedRepo, ws)
	if !ok {
		return
	}
	repo := req.repo

	dropDelta, ok := s.gateDropBaseline(w, r, req)
	if !ok {
		return
	}

	blobKey, err := s.storeRawProfile(r, repo.ID, req.raw)
	if err != nil {
		s.internalError(w, "storing raw profile", err)
		return
	}

	// Forge client for build status, PR comment and diff coverage; nil when
	// the repo has no credentials configured.
	fg, fgErr := s.forgeFor(r.Context(), repo)

	var diffResult *diffcov.Result
	var diffStatus string
	if req.prID != "" {
		diffResult, diffStatus = s.computeDiffCoverage(r.Context(), fg, fgErr, repo, req.prID, req.prof, req.format, req.pathPrefix)
	}

	gate := evaluateGate(repo.Gate, req.totalPct, dropDelta, diffResult)
	upload, files := req.rows(blobKey, diffResult, gate, buildUploadMeta(r, req.filename, len(req.raw), time.Since(start)))
	if err := s.store.CreateUpload(r.Context(), upload, files); err != nil {
		// The raw profile was already written; don't leave it orphaned.
		if delErr := s.blobs.Delete(r.Context(), blobKey); delErr != nil {
			s.log.Error("cleaning up blob after failed upload", "key", blobKey, "err", delErr)
		}
		s.internalError(w, "saving upload", err)
		return
	}

	// Recompute the commit's merged report from every part's latest upload
	// and drive all outward-facing surfaces from it, so a commit uploaded
	// in several parts reports its combined total, not the last part in.
	rc, err := s.recomputeCommitReport(r.Context(), repo, upload)
	if err != nil {
		// The upload row is already committed; a recompute failure (including
		// the bounded-timeout case) returns 500 deliberately so the CI client
		// sees the upload didn't fully land. It self-heals: the next part's
		// upload — or a retry of this one — recomputes the commit again.
		s.internalError(w, "computing merged report", err)
		return
	}

	resp := uploadResponse{
		ID:           upload.ID,
		TotalPct:     rc.upload.TotalPct,
		CoveredStmts: rc.upload.CoveredStmts,
		TotalStmts:   rc.upload.TotalStmts,
		DeltaPct:     rc.delta,
		RepoCreated:  req.repoCreated,
		DiffStatus:   diffStatus,
		Warnings:     rc.warnings,
	}
	if rc.gate.configured {
		resp.Gate = rc.gate.String()
	}
	s.pushToForge(r.Context(), fg, fgErr, repo, upload, rc, &resp)
	if md := rc.upload.DiffCoverage; md != nil {
		pct := md.Percent()
		resp.DiffPct = &pct
		resp.DiffCoveredLines = &md.CoveredLines
		resp.DiffTotalLines = &md.TotalLines
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// uploadRequest is one accepted upload: the multipart form's fields after
// validation and normalization, the repo they resolved to, and the parsed
// profile with its totals. Everything the flow needs from the request is
// read once, here, so the handler never reaches back into r.
type uploadRequest struct {
	repo        *store.Repo
	repoCreated bool // the workspace token registered it just now

	commit     string
	branch     string
	prID       string
	part       string
	format     string
	pathPrefix string

	filename string // as the client named it, for provenance
	raw      []byte
	prof     *profile.Profile

	covered, total int64
	totalPct       float64
}

// readUploadRequest parses the multipart body and validates every field,
// writing the error response itself; a false second return means the
// response is already written. It also resolves the target repo (which a
// workspace token may register on the spot) and rejects a commit that has
// accumulated too many parts, because both answer the same question the
// validation does: is this upload one we accept at all?
func (s *Server) readUploadRequest(w http.ResponseWriter, r *http.Request, authedRepo *store.Repo, ws *store.Workspace) (*uploadRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		httpError(w, http.StatusBadRequest, "invalid multipart form: %v", err)
		return nil, false
	}

	repo, repoCreated, ok := s.resolveUploadRepo(w, r, authedRepo, ws, r.FormValue("repo"))
	if !ok {
		return nil, false
	}
	req := &uploadRequest{
		repo:        repo,
		repoCreated: repoCreated,
		commit:      r.FormValue("commit"),
		branch:      r.FormValue("branch"),
		prID:        r.FormValue("pr_id"),
		pathPrefix:  strings.TrimSuffix(r.FormValue("path_prefix"), "/"),
	}
	if req.commit == "" {
		httpError(w, http.StatusBadRequest, "missing field: commit")
		return nil, false
	}
	if !commitRe.MatchString(req.commit) {
		httpError(w, http.StatusBadRequest, "invalid commit %q: want up to 64 alphanumeric, dot, dash or underscore characters", req.commit)
		return nil, false
	}
	if req.branch == "" {
		req.branch = repo.DefaultBranch
	}

	// A part names one slice of the commit's coverage (backend, frontend,
	// e2e, ...) uploaded from a separate CI job. It is normalized (trimmed
	// and lowercased) before validation so the same logical part from
	// different callers keys the same bucket; the API is called directly,
	// not only through the CLI, so normalization lives here on the server.
	// Omitting it keeps the historical single-upload behaviour: everything
	// lands in "default".
	req.part = strings.ToLower(strings.TrimSpace(r.FormValue("part")))
	if req.part == "" {
		req.part = "default"
	} else if !partRe.MatchString(req.part) {
		httpError(w, http.StatusBadRequest, "invalid part %q: want up to 64 alphanumeric, dot, dash or underscore characters starting with a letter or digit", req.part)
		return nil, false
	}

	// Cap the distinct parts per commit before doing any work, so a runaway
	// part name (e.g. -part $CI_JOB_ID) can't accumulate parts unbounded.
	// Re-uploading an existing part is always allowed — it replaces.
	if parts, err := s.store.CommitParts(r.Context(), repo.ID, req.commit); err != nil {
		s.internalError(w, "counting commit parts", err)
		return nil, false
	} else if len(parts) >= maxPartsPerCommit {
		isNew := !slices.Contains(parts, req.part)
		if isNew {
			httpError(w, http.StatusBadRequest, "commit %s already has %d coverage parts (the maximum); check that the upload's part name is stable across CI jobs", req.commit, len(parts))
			return nil, false
		}
	}

	file, fileHdr, err := r.FormFile("profile")
	if err != nil {
		httpError(w, http.StatusBadRequest, "missing file field: profile")
		return nil, false
	}
	defer file.Close()
	if req.raw, err = io.ReadAll(file); err != nil {
		httpError(w, http.StatusBadRequest, "reading profile: %v", err)
		return nil, false
	}
	req.filename = fileHdr.Filename

	// An explicit format wins; otherwise sniff the content, keeping "go"
	// as the historical default for unrecognizable input.
	req.format = r.FormValue("format")
	if req.format == "" {
		if detected := profile.Detect(req.raw); detected != "" {
			req.format = detected
		} else {
			req.format = "go"
		}
	}
	parser, ok := s.parsers[req.format]
	if !ok {
		httpError(w, http.StatusBadRequest, "unsupported format %q", req.format)
		return nil, false
	}
	if req.prof, err = parser.Parse(bytes.NewReader(req.raw)); err != nil {
		httpError(w, http.StatusUnprocessableEntity, "parsing %s profile: %v", req.format, err)
		return nil, false
	}
	req.covered, req.total = req.prof.Coverage()
	req.totalPct = profile.Percent(req.covered, req.total)
	return req, true
}

// gateDropBaseline returns this upload's coverage difference to the gate's
// baseline, or nil when the drop rule is off or has nothing to compare
// against. The rule always compares against the default branch, so a PR
// cannot lower coverage step by step within tolerance. A false second
// return means the error response is already written.
func (s *Server) gateDropBaseline(w http.ResponseWriter, r *http.Request, req *uploadRequest) (*float64, bool) {
	if req.repo.Gate.MaxCoverageDrop == nil {
		return nil, true
	}
	base, err := s.store.LatestPassedCommitReport(r.Context(), req.repo.ID, req.repo.DefaultBranch, req.commit)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, "loading gate baseline", err)
		return nil, false
	}
	if base == nil {
		return nil, true
	}
	d := req.totalPct - base.TotalPct
	return &d, true
}

// rows turns the request into the upload row and its per-file rows.
//
// The upload row keeps its own single-part gate result (its gate_failed
// column still feeds the per-upload web views); the response, forge
// status, gate and PR comment are driven by the merged report computed
// after the row is stored.
func (req *uploadRequest) rows(blobKey string, diff *diffcov.Result, gate gateResult, meta store.UploadMeta) (*store.Upload, []*store.UploadFile) {
	upload := &store.Upload{
		RepoID:       req.repo.ID,
		CommitSHA:    req.commit,
		Branch:       req.branch,
		PRID:         req.prID,
		Format:       req.format,
		TotalPct:     req.totalPct,
		CoveredStmts: req.covered,
		TotalStmts:   req.total,
		RawBlobKey:   blobKey,
		DiffCoverage: diff,
		GateFailed:   gate.failed(),
		PathPrefix:   req.pathPrefix,
		Part:         req.part,
		Meta:         meta,
	}
	files := make([]*store.UploadFile, 0, len(req.prof.Files))
	for i := range req.prof.Files {
		f := &req.prof.Files[i]
		c, t := f.Coverage()
		files = append(files, &store.UploadFile{
			Path:         f.Path,
			Pct:          profile.Percent(c, t),
			CoveredStmts: c,
			TotalStmts:   t,
			Blocks:       f.Blocks,
		})
	}
	return upload, files
}

// sourceExts maps a profile format to the extensions of source files whose
// absence from the coverage report is worth flagging in diff coverage.
var sourceExts = map[string][]string{
	"go":        {".go"},
	"lcov":      {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".vue", ".svelte"},
	"jacoco":    {".java", ".kt", ".kts", ".scala", ".groovy"},
	"cobertura": {".py", ".cs", ".php", ".cpp", ".cc", ".c"},
}

// computeDiffCoverage fetches the PR diff from the forge and intersects it
// with the parsed profile. Best effort: any failure is reported in the
// returned status, never as an upload error.
func (s *Server) computeDiffCoverage(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, prID string, prof *profile.Profile, format, pathPrefix string) (*diffcov.Result, string) {
	if fgErr != nil {
		return nil, "error: " + fgErr.Error()
	}
	if fg == nil {
		return nil, "skipped: no forge connection"
	}
	diffText, err := fg.GetPRDiff(ctx, repo.Slug, prID)
	if errors.Is(err, forge.ErrNotImplemented) {
		return nil, "skipped: diff not supported by forge"
	}
	if err != nil {
		s.log.Error("fetch PR diff", "repo", repo.Slug, "pr", prID, "err", err)
		return nil, "error: fetching PR diff: " + err.Error()
	}
	added, err := diffcov.ParseUnifiedDiff(strings.NewReader(diffText))
	if err != nil {
		s.log.Error("parse PR diff", "repo", repo.Slug, "pr", prID, "err", err)
		return nil, "error: parsing PR diff: " + err.Error()
	}

	files := make([]diffcov.FileBlocks, 0, len(prof.Files))
	for _, f := range prof.Files {
		files = append(files, diffcov.FileBlocks{Path: f.Path, Blocks: f.Blocks})
	}
	result := diffcov.Compute(files, added, pathPrefix)

	// Keep only source files in the "changed but no coverage data" list;
	// docs, configs etc. are expected to be absent from the profile.
	if exts := sourceExts[format]; len(exts) > 0 {
		var src []string
		for _, p := range result.UnmatchedFiles {
			for _, ext := range exts {
				if strings.HasSuffix(p, ext) {
					src = append(src, p)
					break
				}
			}
		}
		result.UnmatchedFiles = src
	}
	return result, "computed"
}

// commitRe bounds commit identifiers: they appear in forge API paths and
// in blobstore cache keys, so separators are not welcome.
var commitRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// partRe bounds a normalized part name — a canonical lowercase slug that
// starts alphanumeric. It becomes a storage key (and later a flag key), so
// the charset is conservative and the length is bounded. Names are trimmed
// and lowercased before this check, so "Backend" and " backend " reduce to
// the same "backend" and can't split one commit into two parts.
var partRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func (s *Server) storeRawProfile(r *http.Request, repoID int64, raw []byte) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key := fmt.Sprintf("profiles/%d/%s", repoID, hex.EncodeToString(buf))
	if err := s.blobs.Put(r.Context(), key, raw); err != nil {
		return "", err
	}
	return key, nil
}
