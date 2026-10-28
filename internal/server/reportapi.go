package server

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/store"
)

// The read API: a commit's merged report as a versioned, documented
// contract (docs/api.md), for tools and agents rather than for the app. The
// app's /api/ui/ follows the screens and changes with them; this does not.
// It answers what a change left untested — the uncovered changed lines —
// rather than everything a report page draws.
//
// Without a credential it serves what the public report pages already
// show: a repo whose reports are public, or any repo on an open instance.
// With one it serves any repo that credential may upload to — its upload
// token, its workspace's token, or the OIDC token its own CI minted — so
// an agent reads a private repo with what its CI already holds. A session
// cookie is not consulted, and a repo it may not serve is a 404 whether or
// not it exists.

// reportAPIDTO is the read API's answer for one commit. Field names are
// part of the contract: add fields, never rename or repurpose them.
type reportAPIDTO struct {
	Repo   repoRefDTO `json:"repo"`
	Commit string     `json:"commit"`
	Branch string     `json:"branch"`
	PRID   string     `json:"pr_id,omitempty"`
	// UpdatedAt is the commit's latest upload: the report changes until
	// every part has arrived.
	UpdatedAt         time.Time `json:"updated_at"`
	Parts             int       `json:"parts"`
	Coverage          float64   `json:"coverage"`
	CoveredStatements int64     `json:"covered_statements"`
	TotalStatements   int64     `json:"total_statements"`
	GateFailed        bool      `json:"gate_failed"`
	// Base and Delta are null when the commit has nothing to compare with.
	Base  *reportAPIBaseDTO `json:"base"`
	Delta *float64          `json:"delta"`
	// Diff is null for a commit that is not a pull-request build, or whose
	// diff could not be read.
	Diff      *reportAPIDiffDTO `json:"diff"`
	ReportURL string            `json:"report_url"`
}

type reportAPIBaseDTO struct {
	Commit   string  `json:"commit"`
	Coverage float64 `json:"coverage"`
}

type reportAPIDiffDTO struct {
	Coverage     float64 `json:"coverage"`
	CoveredLines int64   `json:"covered_lines"`
	TotalLines   int64   `json:"total_lines"`
	// Uncovered lists the changed files with changed lines no test ran,
	// in diff order. Never null, so a client can range over it.
	Uncovered []reportAPIFileDTO `json:"uncovered"`
	// UnmatchedFiles are changed files the coverage data does not mention.
	UnmatchedFiles []string `json:"unmatched_files"`
}

type reportAPIFileDTO struct {
	Path string `json:"path"`
	// Lines is the human spelling, "49-50, 57-58"; Spans the same lines
	// as inclusive [start, end] pairs.
	Lines string   `json:"lines"`
	Spans [][2]int `json:"spans"`
}

// handleReportAPI serves GET /api/v1/reports/{forge}/{slug...}: the merged
// report of ?commit=, or else the newest report on ?branch= (the repo's
// default branch when that is omitted too).
func (s *Server) handleReportAPI(w http.ResponseWriter, r *http.Request) {
	repo, err := s.store.RepoBySlug(r.Context(), r.PathValue("forge"), r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "loading repo", err)
		return
	}
	public := !s.authEnabled()
	if !public {
		granted, ok := s.reportCredentialGrants(w, r, repo)
		if !ok {
			return
		}
		switch {
		case granted:
		case s.publicReports && repo.ReportsPublic():
			public = true
			// The cached "public" may have flipped on the forge: re-verify
			// an aged answer in the background, as the public report pages do.
			s.pipeline.ReverifyVisibilityIfStale(repo)
		default:
			httpError(w, http.StatusNotFound, "not found")
			return
		}
	}

	var cr *store.CommitReport
	if commit := strings.TrimSpace(r.FormValue("commit")); commit != "" {
		cr, err = s.store.CommitReport(r.Context(), repo.ID, commit)
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "no report for commit %s", commit)
			return
		}
	} else {
		branch := cmp.Or(strings.TrimSpace(r.FormValue("branch")), repo.DefaultBranch)
		var reports []*store.CommitReport
		reports, err = s.store.ListBranchCommitReports(r.Context(), repo.ID, branch, 1)
		if err == nil && len(reports) == 0 {
			httpError(w, http.StatusNotFound, "no report on branch %s", branch)
			return
		}
		if err == nil {
			cr = reports[0]
		}
	}
	if err != nil {
		s.internalError(w, "loading commit report", err)
		return
	}

	dto := reportAPIDTO{
		Repo:              newRepoRefDTO(repo),
		Commit:            cr.CommitSHA,
		Branch:            cr.Branch,
		PRID:              cr.PRID,
		UpdatedAt:         cr.UpdatedAt,
		Parts:             cr.PartCount,
		Coverage:          cr.TotalPct,
		CoveredStatements: cr.CoveredStmts,
		TotalStatements:   cr.TotalStmts,
		GateFailed:        cr.GateFailed,
		Diff:              newReportAPIDiffDTO(cr.DiffCoverage),
		ReportURL:         s.commitReportURL(cr),
	}
	if base := core.CommitBaseline(r.Context(), s.store, repo, cr); base != nil {
		dto.Base = &reportAPIBaseDTO{Commit: base.CommitSHA, Coverage: base.TotalPct}
		dto.Delta = new(cr.TotalPct - base.TotalPct)
	}
	switch {
	case !s.authEnabled():
	case public:
		// The public report pages' caching, for the same reason.
		w.Header().Set("Cache-Control", "public, max-age=60")
	default:
		// Read with a credential: no shared cache may keep it.
		w.Header().Set("Cache-Control", "private, no-store")
	}
	s.writeJSON(w, dto)
}

// reportCredentialGrants reports whether the request's bearer credential
// may upload to repo, and so may read it: false when none is sent or it is
// for another repo, which leaves the public rule to decide. A credential
// that is no credential at all — an unknown token, an OIDC token that does
// not verify — is refused outright rather than read as anonymous, so a
// broken CI secret says so instead of looking like a missing report. A
// false second return means the response is written.
func (s *Server) reportCredentialGrants(w http.ResponseWriter, r *http.Request, repo *store.Repo) (granted, ok bool) {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		return false, true
	}
	if strings.Count(token, ".") == 2 {
		// A JWT: a forge OIDC token. Upload tokens are hex.
		return s.oidcGrantsRead(w, r, repo, token)
	}
	tokRepo, ws, ok := s.lookupUploadToken(w, r, token)
	if !ok {
		return false, false
	}
	if tokRepo != nil {
		return tokRepo.ID == repo.ID, true
	}
	return ws.Forge == repo.Forge && strings.HasPrefix(repo.Slug, ws.Prefix+"/"), true
}

// oidcGrantsRead reports whether a forge OIDC token names repo, by the same
// claims an OIDC upload is mapped by (oidc.go). It never registers a repo:
// a read needs one that is tracked already.
func (s *Server) oidcGrantsRead(w http.ResponseWriter, r *http.Request, repo *store.Repo, raw string) (granted, ok bool) {
	tok, ok := s.verifyOIDC(w, r, raw, "reads")
	if !ok {
		return false, false
	}
	forgeName := s.oidcForge(tok.Issuer)
	if forgeName != repo.Forge {
		return false, true
	}
	switch forgeName {
	case "github":
		return tok.Claim("repository") == repo.Slug, true
	case "gitlab":
		return tok.Claim("project_path") == repo.Slug, true
	}
	// Bitbucket names the repo by UUID only: ask Bitbucket, through the
	// repo's own workspace connection and under the OIDC upload's rate
	// limit, whether that is this repo's.
	want := bitbucketRepoUUID(tok)
	if want == "" {
		return false, true
	}
	if !s.tokenless.allow(repo.Slug, time.Now()) {
		httpError(w, http.StatusTooManyRequests, "OIDC rate limit reached for %s; try again later", repo.Slug)
		return false, false
	}
	ctx := r.Context()
	var fg forge.Forge
	if ws := s.forges.WorkspaceFor(ctx, repo.Slug, "bitbucket"); ws != nil {
		fg = s.forges.Connected(ctx, ws, "bitbucket")
	}
	if fg == nil {
		httpError(w, http.StatusForbidden, "oidc_not_connected: OIDC reads for %s need its workspace connected to Bitbucket; read with the token instead", repo.Slug)
		return false, false
	}
	got, err := fg.GetRepoID(ctx, repo.Slug)
	if err != nil {
		s.log.Error("oidc bitbucket repo id", "repo", repo.Slug, "err", err)
		httpError(w, http.StatusBadGateway, "could not verify the repository identity with Bitbucket; retrying may help")
		return false, false
	}
	return normalizeUUID(got) == normalizeUUID(want), true
}

func newReportAPIDiffDTO(dc *diffcov.Result) *reportAPIDiffDTO {
	if dc == nil {
		return nil
	}
	out := &reportAPIDiffDTO{
		Coverage:       dc.Percent(),
		CoveredLines:   dc.CoveredLines,
		TotalLines:     dc.TotalLines,
		Uncovered:      []reportAPIFileDTO{},
		UnmatchedFiles: append([]string{}, dc.UnmatchedFiles...),
	}
	for _, f := range dc.Files {
		if len(f.UncoveredLines) == 0 {
			continue
		}
		out.Uncovered = append(out.Uncovered, reportAPIFileDTO{
			Path:  f.Path,
			Lines: diffcov.Ranges(f.UncoveredLines),
			Spans: spanPairs(diffcov.LineSpans(f.UncoveredLines)),
		})
	}
	return out
}

// spanPairs spells spans as the contract's [start, end] pairs.
func spanPairs(spans []diffcov.Span) [][2]int {
	out := make([][2]int, len(spans))
	for i, sp := range spans {
		out[i] = [2]int{sp.Start, sp.End}
	}
	return out
}

// commitReportURL is the page the forge surfaces link to for this report:
// its latest upload, with every part merged when there is more than one.
func (s *Server) commitReportURL(cr *store.CommitReport) string {
	url := fmt.Sprintf("%s/uploads/%d", strings.TrimSuffix(s.baseURL, "/"), cr.UploadID)
	if cr.PartCount > 1 {
		url += "?parts=merged"
	}
	return url
}
