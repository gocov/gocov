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
	"sort"
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

// gateResult is the evaluated coverage gate for one upload.
type gateResult struct {
	configured bool
	failures   []string
}

func (g gateResult) failed() bool { return len(g.failures) > 0 }

func (g gateResult) String() string {
	if g.failed() {
		return "failed: " + strings.Join(g.failures, "; ")
	}
	return "passed"
}

// gateEpsilon absorbs float64 division error so coverage exactly at the
// configured threshold never fails the gate (57 of 100 statements is
// 56.999999999999993 in float arithmetic).
const gateEpsilon = 1e-9

// evaluateGate checks the repo's coverage requirements. dropDelta is the
// difference to the latest gate-passing upload on the default branch —
// never a gate-failing upload, so re-running CI cannot launder a failure,
// and never the branch's own history, so a PR cannot ratchet coverage
// down within tolerance push by push. The drop and diff rules are
// fail-open when their inputs are unavailable.
func evaluateGate(gate store.Gate, totalPct float64, dropDelta *float64, diff *diffcov.Result) gateResult {
	res := gateResult{configured: gate.Configured()}
	if gate.MinCoverage != nil && totalPct < *gate.MinCoverage-gateEpsilon {
		res.failures = append(res.failures,
			fmt.Sprintf("total coverage %.4g%% is below the minimum %.4g%%", totalPct, *gate.MinCoverage))
	}
	if gate.MaxCoverageDrop != nil && dropDelta != nil && *dropDelta < -*gate.MaxCoverageDrop-gateEpsilon {
		res.failures = append(res.failures,
			fmt.Sprintf("coverage dropped %.4g%% (allowed %.4g%%)", -*dropDelta, *gate.MaxCoverageDrop))
	}
	if gate.MinDiffCoverage != nil && diff != nil && diff.TotalLines > 0 && diff.Percent() < *gate.MinDiffCoverage-gateEpsilon {
		res.failures = append(res.failures,
			fmt.Sprintf("diff coverage %.4g%% is below the minimum %.4g%%", diff.Percent(), *gate.MinDiffCoverage))
	}
	return res
}

// handleUpload implements POST /api/v1/upload.
//
// Auth: Bearer token — either a per-repo token or a workspace token.
// With a workspace token the repo field is required; unknown repos under
// the workspace prefix are registered automatically. Multipart form: file
// field "profile"; value fields repo, commit (required), branch (defaults
// to the repo's default branch), pr_id (optional), format (default "go").
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

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		httpError(w, http.StatusBadRequest, "invalid multipart form: %v", err)
		return
	}

	repo, repoCreated, ok := s.resolveUploadRepo(w, r, authedRepo, ws, r.FormValue("repo"))
	if !ok {
		return
	}
	commit := r.FormValue("commit")
	if commit == "" {
		httpError(w, http.StatusBadRequest, "missing field: commit")
		return
	}
	if !commitRe.MatchString(commit) {
		httpError(w, http.StatusBadRequest, "invalid commit %q: want up to 64 alphanumeric, dot, dash or underscore characters", commit)
		return
	}
	branch := r.FormValue("branch")
	if branch == "" {
		branch = repo.DefaultBranch
	}
	prID := r.FormValue("pr_id")

	// A part names one slice of the commit's coverage (backend, frontend,
	// e2e, ...) uploaded from a separate CI job. It is normalized (trimmed
	// and lowercased) before validation so the same logical part from
	// different callers keys the same bucket; the API is called directly,
	// not only through the CLI, so normalization lives here on the server.
	// Omitting it keeps the historical single-upload behaviour: everything
	// lands in "default".
	part := strings.ToLower(strings.TrimSpace(r.FormValue("part")))
	if part == "" {
		part = "default"
	} else if !partRe.MatchString(part) {
		httpError(w, http.StatusBadRequest, "invalid part %q: want up to 64 alphanumeric, dot, dash or underscore characters starting with a letter or digit", part)
		return
	}

	// Cap the distinct parts per commit before doing any work, so a runaway
	// part name (e.g. -part $CI_JOB_ID) can't accumulate parts unbounded.
	// Re-uploading an existing part is always allowed — it replaces.
	if parts, err := s.store.CommitParts(r.Context(), repo.ID, commit); err != nil {
		s.internalError(w, "counting commit parts", err)
		return
	} else if len(parts) >= maxPartsPerCommit {
		isNew := !slices.Contains(parts, part)
		if isNew {
			httpError(w, http.StatusBadRequest, "commit %s already has %d coverage parts (the maximum); check that the upload's part name is stable across CI jobs", commit, len(parts))
			return
		}
	}

	file, fileHdr, err := r.FormFile("profile")
	if err != nil {
		httpError(w, http.StatusBadRequest, "missing file field: profile")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(file)
	if err != nil {
		httpError(w, http.StatusBadRequest, "reading profile: %v", err)
		return
	}

	// An explicit format wins; otherwise sniff the content, keeping "go"
	// as the historical default for unrecognizable input.
	format := r.FormValue("format")
	if format == "" {
		if detected := profile.Detect(raw); detected != "" {
			format = detected
		} else {
			format = "go"
		}
	}
	parser, ok := s.parsers[format]
	if !ok {
		httpError(w, http.StatusBadRequest, "unsupported format %q", format)
		return
	}

	prof, err := parser.Parse(bytes.NewReader(raw))
	if err != nil {
		httpError(w, http.StatusUnprocessableEntity, "parsing %s profile: %v", format, err)
		return
	}

	covered, total := prof.Coverage()
	totalPct := profile.Percent(covered, total)

	// The upload row keeps its own single-part gate result (its gate_failed
	// column still feeds the per-upload web views); the response, forge
	// status, gate and PR comment are driven by the merged report computed
	// after the row is stored. The drop rule always compares against the
	// default branch, so a PR cannot lower coverage step by step within
	// tolerance.
	var dropDelta *float64
	if repo.Gate.MaxCoverageDrop != nil {
		base, err := s.store.LatestPassedCommitReport(r.Context(), repo.ID, repo.DefaultBranch, commit)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.internalError(w, "loading gate baseline", err)
			return
		}
		if base != nil {
			d := totalPct - base.TotalPct
			dropDelta = &d
		}
	}

	blobKey, err := s.storeRawProfile(r, repo.ID, raw)
	if err != nil {
		s.internalError(w, "storing raw profile", err)
		return
	}

	// Forge client for build status, PR comment and diff coverage; nil when
	// the repo has no credentials configured.
	fg, fgErr := s.forgeFor(r.Context(), repo)

	pathPrefix := strings.TrimSuffix(r.FormValue("path_prefix"), "/")
	var diffResult *diffcov.Result
	var diffStatus string
	if prID != "" {
		diffResult, diffStatus = s.computeDiffCoverage(r.Context(), fg, fgErr, repo, prID, prof, format, pathPrefix)
	}

	gate := evaluateGate(repo.Gate, totalPct, dropDelta, diffResult)

	upload := &store.Upload{
		RepoID:       repo.ID,
		CommitSHA:    commit,
		Branch:       branch,
		PRID:         prID,
		Format:       format,
		TotalPct:     totalPct,
		CoveredStmts: covered,
		TotalStmts:   total,
		RawBlobKey:   blobKey,
		DiffCoverage: diffResult,
		GateFailed:   gate.failed(),
		PathPrefix:   pathPrefix,
		Part:         part,
		Meta:         buildUploadMeta(r, fileHdr.Filename, len(raw), time.Since(start)),
	}
	files := make([]*store.UploadFile, 0, len(prof.Files))
	for i := range prof.Files {
		f := &prof.Files[i]
		c, t := f.Coverage()
		files = append(files, &store.UploadFile{
			Path:         f.Path,
			Pct:          profile.Percent(c, t),
			CoveredStmts: c,
			TotalStmts:   t,
			Blocks:       f.Blocks,
		})
	}
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
	merged, mergedDelta, mergedGate := rc.upload, rc.delta, rc.gate

	resp := uploadResponse{
		ID:           upload.ID,
		TotalPct:     merged.TotalPct,
		CoveredStmts: merged.CoveredStmts,
		TotalStmts:   merged.TotalStmts,
		DeltaPct:     mergedDelta,
		RepoCreated:  repoCreated,
		DiffStatus:   diffStatus,
		Warnings:     rc.warnings,
	}
	if mergedGate.configured {
		resp.Gate = mergedGate.String()
	}

	// Forge status/insights/comment push after the locked recompute. Two
	// parts of one commit can push concurrently, and forge latency could let
	// an older push land last and pin the commit to a stale status. Serialize
	// the push per commit and gate it on this upload's version (its id, which
	// rises with the most-complete merged state): TryPushStatus runs the push
	// only if it is not older than the last successful one, and records the
	// version only after the push succeeds — so a failed push doesn't burn the
	// version and a later part retries.
	pushCtx, cancel := context.WithTimeout(r.Context(), statusPushTimeout)
	defer cancel()
	pushed, err := s.store.TryPushStatus(pushCtx, repo.ID, upload.CommitSHA, upload.ID, func(ctx context.Context) error {
		resp.BuildStatus = s.pushBuildStatus(ctx, fg, fgErr, repo, merged, mergedDelta, mergedGate)
		resp.CodeInsights = s.pushCodeInsights(ctx, fg, fgErr, repo, merged, mergedDelta, mergedGate)
		resp.PRComment = s.pushPRComment(ctx, fg, fgErr, repo, merged, mergedDelta, mergedGate)
		// The build status gates merges; if it didn't post, signal failure so
		// the version isn't advanced and a later part retries. Insights and
		// PR comment are best effort and don't hold back the version.
		if strings.HasPrefix(resp.BuildStatus, "error") {
			return errStatusPushFailed
		}
		return nil
	})
	switch {
	case errors.Is(err, errStatusPushFailed):
		// resp fields already carry the per-surface outcome; nothing to do.
	case err != nil:
		// Lock/tx failure: the push may not have run. Report it rather than
		// leaving the fields blank.
		s.log.Warn("status push lock", "repo", repo.Slug, "commit", upload.CommitSHA, "err", err)
		if resp.BuildStatus == "" {
			resp.BuildStatus = "error: " + err.Error()
			resp.CodeInsights = "error: " + err.Error()
		}
	case !pushed:
		resp.BuildStatus = "skipped: superseded"
		resp.CodeInsights = "skipped: superseded"
		if merged.PRID != "" {
			resp.PRComment = "skipped: superseded"
		}
	}
	if md := merged.DiffCoverage; md != nil {
		pct := md.Percent()
		resp.DiffPct = &pct
		resp.DiffCoveredLines = &md.CoveredLines
		resp.DiffTotalLines = &md.TotalLines
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// recomputeCommitReport rebuilds the merged report for the upload's commit
// from the latest upload of every part, persists it, and returns the merged
// view that drives the response and the forge side effects. It is
// self-healing: because every upload recomputes the whole commit, a partial
// early state (only the backend part in, say) is corrected in place as the
// remaining parts arrive. The trade-off is a window in which the merged
// numbers are incomplete — see the note on merged reports in docs/parts.md.
//
// The returned upload is synthetic: it carries the merged totals and diff
// coverage to the existing push helpers, with the triggering upload's id so
// the report card and PR comment link back to it.
type mergedRecompute struct {
	upload   *store.Upload
	delta    *float64
	gate     gateResult
	warnings []string // surfaced to the uploader, e.g. conservative diff merges
}

// recomputeTimeout bounds a single recompute so a saturated connection pool
// fails the upload fast instead of hanging a CI client indefinitely.
const recomputeTimeout = 30 * time.Second

// statusPushTimeout bounds the serialized forge push: TryPushStatus holds a
// per-commit lock (and its connection) across the forge HTTP calls, so a
// hung forge must not pin them.
const statusPushTimeout = 20 * time.Second

// errStatusPushFailed signals TryPushStatus that the build-status push did
// not post, so the status version must not advance and a later part retries.
// It never surfaces as an upload error — the response carries the per-surface
// outcome.
var errStatusPushFailed = errors.New("build status push failed")

func (s *Server) recomputeCommitReport(ctx context.Context, repo *store.Repo, u *store.Upload) (*mergedRecompute, error) {
	ctx, cancel := context.WithTimeout(ctx, recomputeTimeout)
	defer cancel()

	// The whole recompute — read every part, merge, upsert — runs inside one
	// locked transaction, serialized per commit against concurrent uploads
	// (parallel CI jobs are the point) so it cannot interleave with or
	// clobber a newer recompute and drop a part.
	var result *mergedRecompute
	err := s.store.WithCommitReportTx(ctx, repo.ID, u.CommitSHA, func(ctx context.Context, tx store.CommitTx) error {
		parts, err := tx.LatestUploadsPerPart(ctx, repo.ID, u.CommitSHA)
		if err != nil {
			return fmt.Errorf("loading commit parts: %w", err)
		}

		profiles := make([]*profile.Profile, 0, len(parts))
		diffs := make([]*diffcov.Result, 0, len(parts))
		for _, p := range parts {
			files, err := tx.UploadFiles(ctx, p.ID)
			if err != nil {
				return fmt.Errorf("loading part files: %w", err)
			}
			prof := &profile.Profile{Files: make([]profile.File, 0, len(files))}
			for _, f := range files {
				prof.Files = append(prof.Files, profile.File{Path: f.Path, Blocks: f.Blocks})
			}
			profiles = append(profiles, prof)
			if p.DiffCoverage != nil {
				diffs = append(diffs, p.DiffCoverage)
			}
		}

		merged := profile.Merge(profiles...)
		covered, total := merged.Coverage()
		totalPct := profile.Percent(covered, total)
		mergedDiff, diffConflicts := diffcov.Merge(diffs...)
		var warnings []string
		if len(diffConflicts) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"diff coverage merged conservatively for %d changed file(s) whose parts disagree on their changed lines (%s); merged coverage is a safe lower bound",
				len(diffConflicts), strings.Join(diffConflicts, ", ")))
		}

		// Delta vs the previous gate-passing merged report on the branch,
		// falling back to the default branch for first-time feature branches.
		// The commit's own report is skipped so an earlier part is never its
		// own baseline.
		var deltaPct *float64
		prev, err := tx.LatestPassedCommitReport(ctx, repo.ID, u.Branch, u.CommitSHA)
		if errors.Is(err, store.ErrNotFound) && u.Branch != repo.DefaultBranch {
			prev, err = tx.LatestPassedCommitReport(ctx, repo.ID, repo.DefaultBranch, u.CommitSHA)
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("loading baseline report: %w", err)
		}
		if prev != nil {
			d := totalPct - prev.TotalPct
			deltaPct = &d
		}

		// The gate drop rule always compares against the default branch's
		// latest passing merged report, so a PR cannot ratchet coverage down
		// part by part within tolerance.
		var dropDelta *float64
		if repo.Gate.MaxCoverageDrop != nil {
			base, err := tx.LatestPassedCommitReport(ctx, repo.ID, repo.DefaultBranch, u.CommitSHA)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("loading gate baseline report: %w", err)
			}
			if base != nil {
				d := totalPct - base.TotalPct
				dropDelta = &d
			}
		}

		gate := evaluateGate(repo.Gate, totalPct, dropDelta, mergedDiff)

		cr := &store.CommitReport{
			RepoID:       repo.ID,
			CommitSHA:    u.CommitSHA,
			Branch:       u.Branch,
			PRID:         u.PRID,
			TotalPct:     totalPct,
			CoveredStmts: covered,
			TotalStmts:   total,
			GateFailed:   gate.failed(),
			DiffCoverage: mergedDiff,
			PartCount:    len(parts),
			UploadID:     u.ID,
		}
		if err := tx.UpsertCommitReport(ctx, cr); err != nil {
			return fmt.Errorf("saving merged report: %w", err)
		}

		mergedUpload := &store.Upload{
			ID:           u.ID,
			RepoID:       repo.ID,
			CommitSHA:    u.CommitSHA,
			Branch:       u.Branch,
			PRID:         u.PRID,
			TotalPct:     totalPct,
			CoveredStmts: covered,
			TotalStmts:   total,
			DiffCoverage: mergedDiff,
		}
		result = &mergedRecompute{upload: mergedUpload, delta: deltaPct, gate: gate, warnings: warnings}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// forgeFor builds a forge client for the repo through the workspace's
// one-click connection (GitHub App installation, Bitbucket grant or
// GitLab grant). Returns (nil, nil) when the repo's workspace has no
// connection — there is no manual-credential fallback.
func (s *Server) forgeFor(ctx context.Context, repo *store.Repo) (forge.Forge, error) {
	// The workspace is looked up lazily: only when a connection could
	// apply, so a forge that supports no one-click connect skips the
	// query entirely.
	if s.oneClickCapable(repo.Forge) {
		ws := s.repoWorkspace(ctx, repo.Slug, repo.Forge)
		if fg := s.connectedForge(ctx, ws, repo.Forge); fg != nil {
			return fg, nil
		}
	}
	return nil, nil
}

// oneClickCapable reports whether a one-click connection could supply
// credentials for the forge — the gate for the extra workspace lookup.
func (s *Server) oneClickCapable(forgeName string) bool {
	return (s.githubApp != nil && forgeName == "github") ||
		(s.bbConnect != nil && forgeName == "bitbucket") ||
		(s.glConnect != nil && forgeName == "gitlab")
}

// connectedForge returns the workspace's one-click-connected client —
// GitHub App installation, Bitbucket grant or GitLab grant — or nil,
// the top link of the credential chain (D4/D7).
func (s *Server) connectedForge(ctx context.Context, ws *store.Workspace, forgeName string) forge.Forge {
	if fg := s.installationForge(ctx, ws, forgeName); fg != nil {
		return fg
	}
	if fg := s.grantForge(ctx, ws, forgeName); fg != nil {
		return fg
	}
	return s.gitlabGrantForge(ctx, ws, forgeName)
}

// repoWorkspace returns the workspace owning the slug's prefix, nil when
// there is none. Prefixes are tried longest first, so a repo below a
// registered GitLab subgroup resolves to that subgroup's workspace, not a
// same-named ancestor. A lookup failure only degrades down the credential
// chain — forge surfaces are best-effort everywhere else too. The forge
// must match: prefixes are globally unique, and a same-named workspace
// on another forge must not lend its secrets or its installation.
func (s *Server) repoWorkspace(ctx context.Context, slug, forgeName string) *store.Workspace {
	for _, prefix := range slugPrefixes(slug) {
		ws, err := s.store.WorkspaceByPrefix(ctx, prefix)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			s.log.Error("workspace lookup", "repo", slug, "err", err)
			return nil
		}
		if ws.Forge != forgeName {
			return nil
		}
		return ws
	}
	return nil
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

// lookupUploadToken authenticates the Bearer token as either a per-repo
// token or a workspace token, writing the error response itself. Runs
// before the request body is parsed.
func (s *Server) lookupUploadToken(w http.ResponseWriter, r *http.Request, token string) (*store.Repo, *store.Workspace, bool) {
	ctx := r.Context()
	repo, err := s.store.RepoByToken(ctx, token)
	if err == nil {
		return repo, nil, true
	}
	if !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, "looking up token", err)
		return nil, nil, false
	}
	ws, err := s.store.WorkspaceByToken(ctx, token)
	if err == nil {
		return nil, ws, true
	}
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusUnauthorized, "invalid token")
		return nil, nil, false
	}
	s.internalError(w, "looking up workspace token", err)
	return nil, nil, false
}

// repoNameRe bounds the repo part of auto-registered slugs: one path
// segment, conservative charset, sane length.
var repoNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// maxRepoNameSegments bounds a GitLab repo name's depth below the
// workspace; GitLab itself caps group nesting at 20 levels.
const maxRepoNameSegments = 21

// validRepoName validates the repo part of a workspace-token slug.
// GitLab projects can sit in subgroups below the registered namespace, so
// a gitlab name may span several path segments; other forges take exactly
// one.
func validRepoName(forgeName, name string) bool {
	segments := strings.Split(name, "/")
	if forgeName != "gitlab" && len(segments) != 1 {
		return false
	}
	if len(segments) > maxRepoNameSegments {
		return false
	}
	for _, s := range segments {
		// repoNameRe's charset admits "." and ".." — as path segments they
		// are traversal, not names.
		if s == "." || s == ".." || !repoNameRe.MatchString(s) {
			return false
		}
	}
	return true
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

// resolveUploadRepo maps the authenticated token to the target repo,
// writing the error response itself on failure. Workspace tokens require
// the repo slug, must match the workspace prefix, and register unknown
// repos on the fly.
func (s *Server) resolveUploadRepo(w http.ResponseWriter, r *http.Request, repo *store.Repo, ws *store.Workspace, slug string) (_ *store.Repo, created, ok bool) {
	ctx := r.Context()
	if repo != nil {
		if slug != "" && slug != repo.Slug {
			httpError(w, http.StatusForbidden, "token is for repo %q, not %q", repo.Slug, slug)
			return nil, false, false
		}
		return repo, false, true
	}

	if slug == "" {
		httpError(w, http.StatusBadRequest, "workspace tokens require the repo field")
		return nil, false, false
	}
	name, matched := strings.CutPrefix(slug, ws.Prefix+"/")
	if !matched {
		httpError(w, http.StatusForbidden, "token is for workspace %q, not %q", ws.Prefix, slug)
		return nil, false, false
	}
	if !validRepoName(ws.Forge, name) {
		httpError(w, http.StatusBadRequest, "invalid repo name %q under workspace %q", slug, ws.Prefix)
		return nil, false, false
	}

	repo, err := s.store.RepoBySlug(ctx, slug)
	if err == nil {
		return repo, false, true
	}
	if !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, "looking up repo", err)
		return nil, false, false
	}
	repo, err = s.autoCreateRepo(ctx, ws, slug)
	if errors.Is(err, forge.ErrRepoNotFound) {
		httpError(w, http.StatusNotFound, "repo %q not found on %s", slug, ws.Forge)
		return nil, false, false
	}
	if err != nil {
		s.internalError(w, "auto-registering repo", err)
		return nil, false, false
	}
	return repo, true, true
}

// autoCreateRepo registers a repo first seen through a workspace token.
// The default branch is asked from the forge when the workspace has a
// one-click connection, then falls back to the workspace default and
// finally to "main". A forge that positively says the repo does not
// exist aborts the registration (ErrRepoNotFound), so a leaked workspace
// token cannot fill the dashboard with invented repos.
func (s *Server) autoCreateRepo(ctx context.Context, ws *store.Workspace, slug string) (*store.Repo, error) {
	branch := ""
	fg := s.connectedForge(ctx, ws, ws.Forge)
	if fg != nil {
		b, err := fg.GetDefaultBranch(ctx, slug)
		switch {
		case err == nil && b != "":
			branch = b
		case errors.Is(err, forge.ErrRepoNotFound):
			return nil, err
		case err != nil && !errors.Is(err, forge.ErrNotImplemented):
			// Transient forge trouble must not block a legitimate first
			// upload; fall back to the workspace default branch.
			s.log.Warn("get default branch", "repo", slug, "err", err)
		}
	}
	if branch == "" {
		branch = ws.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}

	token, err := newToken()
	if err != nil {
		return nil, err
	}
	repo := &store.Repo{
		Forge:         ws.Forge,
		Slug:          slug,
		Token:         token,
		DefaultBranch: branch,
		Gate:          ws.Gate,
	}
	if err := s.store.CreateRepo(ctx, repo); err != nil {
		// A concurrent first upload may have won the race; use its repo.
		if existing, lookupErr := s.store.RepoBySlug(ctx, slug); lookupErr == nil {
			return existing, nil
		}
		return nil, err
	}
	s.log.Info("auto-registered repo", "slug", slug, "default_branch", branch, "workspace", ws.Prefix)
	return repo, nil
}

// newToken generates an upload token for auto-registered repos.
func newToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

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

// pushBuildStatus posts a "coverage: X% (±Y)" build status to the repo's
// forge; a failed coverage gate turns the state into FAILED so the forge
// can block the merge. Best effort: push failures are reported in the
// response but do not fail the upload.
func (s *Server) pushBuildStatus(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, u *store.Upload, deltaPct *float64, gate gateResult) string {
	if fgErr != nil {
		return "error: " + fgErr.Error()
	}
	if fg == nil {
		return "skipped"
	}

	desc := fmt.Sprintf("coverage: %.1f%%", u.TotalPct)
	if deltaPct != nil {
		desc += fmt.Sprintf(" (%+.1f%%)", *deltaPct)
	}
	state := forge.StateSuccessful
	if gate.failed() {
		state = forge.StateFailed
		// Forge description fields are short; one reason has to do.
		desc += " — " + gate.failures[0]
	}
	status := forge.BuildStatus{
		Key:         "gocov/coverage",
		State:       state,
		Name:        "gocov",
		Description: desc,
		URL:         s.uploadURL(u),
	}
	if err := fg.PostBuildStatus(ctx, repo.Slug, u.CommitSHA, status); err != nil {
		s.log.Error("post build status", "repo", repo.Slug, "commit", u.CommitSHA, "err", err)
		return "error: " + err.Error()
	}
	return "posted"
}

// insightsMaxAnnotations caps report annotations at the forge API's
// per-request limit, keeping the whole publish to one bulk request.
const insightsMaxAnnotations = 100

// pushCodeInsights attaches a coverage report card to the commit and, for
// PR uploads, annotates uncovered changed lines inline in the diff. Best
// effort like the build status: failures land in the response field and
// the log, never in the upload result.
func (s *Server) pushCodeInsights(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, u *store.Upload, deltaPct *float64, gate gateResult) string {
	if fgErr != nil {
		return "error: " + fgErr.Error()
	}
	if fg == nil {
		s.log.Debug("code insights skipped: no forge connection", "repo", repo.Slug)
		return "skipped"
	}
	report, annotations := s.insightsReport(u, deltaPct, gate)
	err := fg.PublishReport(ctx, repo.Slug, u.CommitSHA, report, annotations)
	if errors.Is(err, forge.ErrNotImplemented) {
		// A wrapped sentinel carries the forge's reason (e.g. GitHub
		// check runs being closed to the credential type) — worth
		// surfacing, unlike the bare "this forge has no such surface".
		if err == forge.ErrNotImplemented {
			return "skipped"
		}
		s.log.Info("code insights unavailable", "repo", repo.Slug, "reason", err)
		return "skipped: " + err.Error()
	}
	if err != nil {
		s.log.Warn("publish code insights report", "repo", repo.Slug, "commit", u.CommitSHA, "err", err)
		return "error: " + err.Error()
	}
	return "posted"
}

// insightsReport builds the report card and its annotations. The data
// fields stay well under the forge API's cap of ten; annotations exist
// only for PR uploads, and only on uncovered changed lines.
func (s *Server) insightsReport(u *store.Upload, deltaPct *float64, gate gateResult) (forge.Report, []forge.Annotation) {
	data := []forge.ReportData{
		{Title: "Total coverage", Type: forge.DataPercentage, Value: u.TotalPct},
	}
	if deltaPct != nil {
		data = append(data, forge.ReportData{
			Title: "Change vs base", Type: forge.DataText, Value: fmt.Sprintf("%+.1f%%", *deltaPct)})
	}

	details := "Test coverage uploaded by gocov."
	var annotations []forge.Annotation
	if dc := u.DiffCoverage; dc != nil {
		if dc.TotalLines == 0 {
			details = "No executable lines were changed."
		} else {
			data = append(data,
				forge.ReportData{Title: "Diff coverage", Type: forge.DataPercentage, Value: dc.Percent()},
				forge.ReportData{Title: "Uncovered changed lines", Type: forge.DataNumber,
					Value: float64(dc.TotalLines - dc.CoveredLines)},
			)
			var dropped int
			annotations, dropped = insightsAnnotations(dc)
			details = fmt.Sprintf("%d of %d changed lines are covered by tests.", dc.CoveredLines, dc.TotalLines)
			if dropped > 0 {
				details += fmt.Sprintf(" +%d more uncovered ranges are not annotated — the PR comment lists them all.", dropped)
			}
		}
	}
	data = append(data, forge.ReportData{
		Title: "Statements", Type: forge.DataText, Value: fmt.Sprintf("%d / %d", u.CoveredStmts, u.TotalStmts)})

	result := ""
	if gate.configured {
		data = append(data, forge.ReportData{Title: "Gate", Type: forge.DataText, Value: gate.String()})
		if gate.failed() {
			result = forge.ReportFailed
		} else {
			result = forge.ReportPassed
		}
	}
	if dc := u.DiffCoverage; dc != nil {
		data = appendPerFileData(data, dc)
	}

	return forge.Report{
		Title:   "gocov coverage",
		Details: details,
		Result:  result,
		Link:    s.uploadURL(u),
		Data:    data,
	}, annotations
}

// insightsMaxDataFields is the forge API's cap on report data fields.
const insightsMaxDataFields = 10

// appendPerFileData fills the remaining data-field budget with a per-file
// summary of the worst-covered changed files, lowest diff coverage first.
// Fully covered files say nothing a reviewer needs, so they never claim
// a field.
func appendPerFileData(data []forge.ReportData, dc *diffcov.Result) []forge.ReportData {
	var files []diffcov.FileCoverage
	for _, f := range dc.Files {
		if len(f.UncoveredLines) > 0 {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		pi := float64(files[i].CoveredLines) * float64(files[j].TotalLines)
		pj := float64(files[j].CoveredLines) * float64(files[i].TotalLines)
		if pi != pj {
			return pi < pj
		}
		return files[i].Path < files[j].Path
	})
	for _, f := range files {
		if len(data) >= insightsMaxDataFields {
			break
		}
		data = append(data, forge.ReportData{
			Title: dataFieldPath(f.Path),
			Type:  forge.DataPercentage,
			Value: 100 * float64(f.CoveredLines) / float64(f.TotalLines),
		})
	}
	return data
}

// dataFieldPath keeps report data titles readable for deep paths: long
// ones keep their tail, which carries the file name.
func dataFieldPath(p string) string {
	const max = 60
	r := []rune(p)
	if len(r) <= max {
		return p
	}
	return "…" + string(r[len(r)-max+1:])
}

// insightsAnnotations turns the diff-coverage result into one annotation
// per contiguous uncovered range, anchored at the range start and ordered
// by file path (dc.Files is path-sorted). Ranges beyond the cap are
// counted, not annotated.
func insightsAnnotations(dc *diffcov.Result) (anns []forge.Annotation, dropped int) {
	// Whole-file findings first — a changed source file with no coverage
	// data at all. File-level (no line), so the forge pins them to the
	// file header in the diff. They are few and salient, which is why
	// they get the budget before line ranges.
	for _, p := range dc.UnmatchedFiles {
		if len(anns) == insightsMaxAnnotations {
			dropped++
			continue
		}
		anns = append(anns, forge.Annotation{
			Path:    p,
			Summary: "This changed file has no coverage data — nothing in it appears to be tested",
		})
	}
	for _, f := range dc.Files {
		lines := f.UncoveredLines
		for i := 0; i < len(lines); {
			j := i
			for j+1 < len(lines) && lines[j+1] == lines[j]+1 {
				j++
			}
			if len(anns) == insightsMaxAnnotations {
				dropped++
			} else {
				summary := fmt.Sprintf("Line %d of this change is not covered by tests", lines[i])
				if j > i {
					summary = fmt.Sprintf("Lines %d–%d of this change are not covered by tests", lines[i], lines[j])
				}
				anns = append(anns, forge.Annotation{Path: f.Path, Line: lines[i], EndLine: lines[j], Summary: summary})
			}
			i = j + 1
		}
	}
	return anns, dropped
}

// prCommentMarker identifies gocov's own comment on a PR; every body
// built by prCommentBody starts with it, so repeated uploads update the
// existing comment instead of stacking new ones.
const prCommentMarker = "**gocov**"

// pushPRComment posts or updates the coverage summary comment on the pull
// request. Returns "" for non-PR uploads so the field is omitted from the
// response.
func (s *Server) pushPRComment(ctx context.Context, fg forge.Forge, fgErr error, repo *store.Repo, u *store.Upload, deltaPct *float64, gate gateResult) string {
	if u.PRID == "" {
		return ""
	}
	if fgErr != nil {
		return "error: " + fgErr.Error()
	}
	if fg == nil {
		return "skipped"
	}
	body := s.prCommentBody(u, deltaPct, gate)

	// Best effort update-in-place: any failure falls back to posting a
	// fresh comment, which is never worse than the old behavior.
	commentID, err := fg.FindPRComment(ctx, repo.Slug, u.PRID, prCommentMarker)
	if err != nil && !errors.Is(err, forge.ErrNotImplemented) {
		s.log.Warn("find PR comment", "repo", repo.Slug, "pr", u.PRID, "err", err)
	}
	if commentID != "" {
		if err := fg.UpdatePRComment(ctx, repo.Slug, u.PRID, commentID, body); err == nil {
			return "updated"
		} else {
			s.log.Warn("update PR comment", "repo", repo.Slug, "pr", u.PRID, "comment", commentID, "err", err)
		}
	}

	if err := fg.PostPRComment(ctx, repo.Slug, u.PRID, body); err != nil {
		s.log.Error("post PR comment", "repo", repo.Slug, "pr", u.PRID, "err", err)
		return "error: " + err.Error()
	}
	return "posted"
}

// prCommentMaxFiles caps the uncovered-lines table in PR comments.
const prCommentMaxFiles = 20

func (s *Server) prCommentBody(u *store.Upload, deltaPct *float64, gate gateResult) string {
	var sb strings.Builder
	short := u.CommitSHA
	if len(short) > 12 {
		short = short[:12]
	}
	fmt.Fprintf(&sb, "**gocov** report for `%s`\n\n", short)
	fmt.Fprintf(&sb, "- Total coverage: **%.1f%%**", u.TotalPct)
	if deltaPct != nil {
		fmt.Fprintf(&sb, " (%+.1f%%)", *deltaPct)
	}
	sb.WriteString("\n")
	if gate.configured {
		if gate.failed() {
			fmt.Fprintf(&sb, "- Gate: ❌ %s\n", strings.Join(gate.failures, "; "))
		} else {
			sb.WriteString("- Gate: ✅ passed\n")
		}
	}

	if dc := u.DiffCoverage; dc != nil {
		if dc.TotalLines == 0 {
			sb.WriteString("- Diff coverage: no executable lines changed\n")
		} else {
			fmt.Fprintf(&sb, "- Diff coverage: **%.1f%%** (%d/%d changed lines covered)\n",
				dc.Percent(), dc.CoveredLines, dc.TotalLines)
		}

		var uncovered []diffcov.FileCoverage
		for _, f := range dc.Files {
			if len(f.UncoveredLines) > 0 {
				uncovered = append(uncovered, f)
			}
		}
		if len(uncovered) > 0 {
			sb.WriteString("\nUncovered changed lines:\n\n| File | Lines |\n| --- | --- |\n")
			for i, f := range uncovered {
				if i == prCommentMaxFiles {
					fmt.Fprintf(&sb, "| … | and %d more files |\n", len(uncovered)-prCommentMaxFiles)
					break
				}
				fmt.Fprintf(&sb, "| `%s` | %s |\n", mdPath(f.Path), diffcov.Ranges(f.UncoveredLines))
			}
		}
		if n := len(dc.UnmatchedFiles); n > 0 {
			shown := dc.UnmatchedFiles
			if n > prCommentMaxFiles {
				shown = shown[:prCommentMaxFiles]
			}
			escaped := make([]string, len(shown))
			for i, p := range shown {
				escaped[i] = mdPath(p)
			}
			fmt.Fprintf(&sb, "\nChanged files without coverage data: `%s`",
				strings.Join(escaped, "`, `"))
			if n > prCommentMaxFiles {
				fmt.Fprintf(&sb, " and %d more", n-prCommentMaxFiles)
			}
			sb.WriteString("\n")
		}
	}

	fmt.Fprintf(&sb, "\n[Full report](%s)\n", s.uploadURL(u))
	return sb.String()
}

func (s *Server) uploadURL(u *store.Upload) string {
	return fmt.Sprintf("%s/uploads/%d", strings.TrimSuffix(s.baseURL, "/"), u.ID)
}

// mdPath neutralizes characters that would break the markdown table or the
// surrounding code span in PR comments. Paths come from the PR diff.
var mdPathReplacer = strings.NewReplacer("`", "'", "|", "\\|", "\n", " ", "\r", " ")

func mdPath(p string) string {
	return mdPathReplacer.Replace(p)
}

func httpError(w http.ResponseWriter, code int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf(format, args...)})
}

func (s *Server) internalError(w http.ResponseWriter, msg string, err error) {
	s.log.Error(msg, "err", err)
	httpError(w, http.StatusInternalServerError, "internal error")
}
