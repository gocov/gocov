package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/readapi"
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
		case s.servesPublicly(repo):
			public = true
		default:
			httpError(w, http.StatusNotFound, "not found")
			return
		}
	}

	dto, err := s.loadReport(r.Context(), repo, r.FormValue("commit"), r.FormValue("branch"))
	var none noReportError
	if errors.As(err, &none) {
		httpError(w, http.StatusNotFound, "%s", none)
		return
	}
	if err != nil {
		s.internalError(w, "loading commit report", err)
		return
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

// servesPublicly reports whether repo's reports may be read with no
// credential on an instance with sign-in: the public report pages' rule.
// Serving on it re-verifies an aged "public" in the background, as those
// pages do, since the forge may have flipped it since.
func (s *Server) servesPublicly(repo *store.Repo) bool {
	if !s.publicReports || !repo.ReportsPublic() {
		return false
	}
	s.pipeline.ReverifyVisibilityIfStale(repo)
	return true
}

// noReportError says which report a read named that does not exist.
type noReportError string

func (e noReportError) Error() string { return string(e) }

// loadReport builds the read API's answer: the merged report of commit,
// or else the newest report on branch (the repo's default branch when that
// is empty too). A report that does not exist is a noReportError.
func (s *Server) loadReport(ctx context.Context, repo *store.Repo, commit, branch string) (*readapi.Report, error) {
	var cr *store.CommitReport
	if commit = strings.TrimSpace(commit); commit != "" {
		var err error
		cr, err = s.store.CommitReport(ctx, repo.ID, commit)
		if errors.Is(err, store.ErrNotFound) {
			return nil, noReportError("no report for commit " + commit)
		}
		if err != nil {
			return nil, err
		}
	} else {
		branch = cmp.Or(strings.TrimSpace(branch), repo.DefaultBranch)
		reports, err := s.store.ListBranchCommitReports(ctx, repo.ID, branch, 1)
		if err != nil {
			return nil, err
		}
		if len(reports) == 0 {
			return nil, noReportError("no report on branch " + branch)
		}
		cr = reports[0]
	}
	rep := &readapi.Report{
		Repo:              readapi.Repo{Forge: repo.Forge, Slug: repo.Slug},
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
	if base := core.CommitBaseline(ctx, s.store, repo, cr); base != nil {
		rep.Base = &readapi.Base{Commit: base.CommitSHA, Coverage: base.TotalPct}
		rep.Delta = new(cr.TotalPct - base.TotalPct)
	}
	return rep, nil
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

func newReportAPIDiffDTO(dc *diffcov.Result) *readapi.Diff {
	if dc == nil {
		return nil
	}
	out := &readapi.Diff{
		Coverage:       dc.Percent(),
		CoveredLines:   dc.CoveredLines,
		TotalLines:     dc.TotalLines,
		Uncovered:      []readapi.File{},
		UnmatchedFiles: append([]string{}, dc.UnmatchedFiles...),
	}
	for _, f := range dc.Files {
		if len(f.UncoveredLines) == 0 {
			continue
		}
		out.Uncovered = append(out.Uncovered, readapi.File{
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
