// The upload endpoint: the one write path into gocov. A CI job posts a
// coverage profile here, and this file runs it end to end — authenticate,
// read and validate the request, parse the profile, store it, merge the
// commit's parts, then report back to the uploader. The steps it delegates
// live next door: gate.go, merge.go, forgepush.go and uploadrepo.go.

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/core"
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
// the workspace prefix are registered automatically. Without any token
// the request may instead claim a running GitHub Actions pull_request
// workflow, verified through the workspace's App installation (tokenless
// fork-PR uploads — tokenless.go). Multipart form: file field "profile";
// value fields repo, commit (required), branch (defaults to the repo's
// default branch), pr_id (optional), format (default "go").
//
// The body below is the flow and nothing else — authenticate, read the
// request, store what arrived, merge the commit's parts, answer the
// uploader, tell the forge — with each step's detail one call away.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	token, hasToken := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	var authedRepo *store.Repo
	var ws *store.Workspace
	var claim *tokenlessClaim // non-nil on the tokenless path
	if hasToken && token != "" {
		// Authenticate before touching the body so invalid tokens cost a
		// lookup, not a 64MB multipart parse. The tokenless path cannot
		// have that luxury: its credentials are form fields.
		var ok bool
		if authedRepo, ws, ok = s.lookupUploadToken(w, r, token); !ok {
			return
		}
	} else {
		var ok bool
		if claim, authedRepo, ok = s.authTokenless(w, r); !ok {
			return
		}
	}
	req, ok := s.readUploadRequest(w, r, authedRepo, ws)
	if !ok {
		return
	}
	meta := buildUploadMeta(r, req.filename, len(req.raw), time.Since(start))
	if claim != nil {
		// Server-set only: the flag drives the "unverified contributor
		// upload" badge and must not be settable through a form field.
		meta.Tokenless = true
		// One accept per (run, attempt, part): first verified upload wins
		// the triple, replays are refused. Claimed only after verification
		// so an unverifiable request cannot squat a real run's slot.
		won, err := s.store.ClaimTokenlessUpload(r.Context(), req.repo.ID, claim.runID, claim.runAttempt, req.part)
		if err != nil {
			s.internalError(w, "claiming tokenless upload", err)
			return
		}
		if !won {
			httpError(w, http.StatusConflict, "workflow run %d (attempt %d) already uploaded part %q; ignoring the duplicate", claim.runID, claim.runAttempt, req.part)
			return
		}
	}
	res, err := s.pipeline.Accept(r.Context(), core.Submission{
		Repo:       req.repo,
		Commit:     req.commit,
		Branch:     req.branch,
		PRID:       req.prID,
		Part:       req.part,
		Format:     req.format,
		PathPrefix: req.pathPrefix,
		Raw:        req.raw,
		Profile:    req.prof,
		Meta:       meta,
	})
	if err != nil {
		if claim != nil {
			// The upload did not land; free the triple so the CI job's
			// retry is not locked out by this failure.
			if relErr := s.store.ReleaseTokenlessUpload(r.Context(), req.repo.ID, claim.runID, claim.runAttempt, req.part); relErr != nil {
				s.log.Error("releasing tokenless claim", "repo", req.repo.Slug, "run", claim.runID, "err", relErr)
			}
		}
		s.internalError(w, "accepting upload", err)
		return
	}

	resp := uploadResponse{
		ID:           res.Upload.ID,
		TotalPct:     res.Merged.Upload.TotalPct,
		CoveredStmts: res.Merged.Upload.CoveredStmts,
		TotalStmts:   res.Merged.Upload.TotalStmts,
		DeltaPct:     res.Merged.Delta,
		RepoCreated:  req.repoCreated,
		DiffStatus:   res.DiffStatus,
		Warnings:     res.Merged.Warnings,
		BuildStatus:  res.Push.BuildStatus,
		CodeInsights: res.Push.CodeInsights,
		PRComment:    res.Push.PRComment,
	}
	if res.Merged.Verdict.Configured {
		resp.Gate = res.Merged.Verdict.String()
	}
	if md := res.Merged.Upload.DiffCoverage; md != nil {
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
	return req, true
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
