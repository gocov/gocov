// Tokenless OIDC uploads: authenticating an upload that carries no bearer
// token and no fork-PR run claim, but a forge-minted OIDC identity token.
// A repo owner's own CI (push, same-repo PR) asks the forge for a
// short-lived JWT scoped to gocov's audience and sends it in place of the
// pasted GOCOV_TOKEN; this file verifies that JWT (internal/oidc has the
// protocol) and maps it to a tracked repo. Unlike a fork-PR tokenless
// upload (tokenless.go), the result is a fully verified upload — the forge
// signed the token, so there is no "unverified" badge.
//
// How the token names its repo differs per forge, so mapping does too:
// GitHub's token carries the slug directly ("repository"), while
// Bitbucket's carries an opaque UUID — resolved against the tracked repo's
// own forge-reported id, so the untrusted slug the request also sends can
// only ever agree with the signed identity, never override it.
package server

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/oidc"
	"github.com/gocov/gocov/internal/store"
)

// gitHubActionsIssuer is the GitHub Actions OIDC issuer. Its identity tokens
// carry a "repository" claim (owner/name) that names the repo a run belongs
// to — the claim we map to a tracked repo.
const gitHubActionsIssuer = "https://token.actions.githubusercontent.com"

// gitLabDotComIssuer is gitlab.com's OIDC issuer, and the default trusted
// GitLab issuer. GitLab CI ID tokens carry a "project_path" claim
// (group/project) — the slug we map to a tracked repo. A self-managed
// GitLab issues under its own URL; setting GOCOV_OIDC_ISSUERS
// (server.Config.OIDCIssuers) replaces this default with those instances.
const gitLabDotComIssuer = "https://gitlab.com"

// bitbucketIssuerPath is the fixed path template of a Bitbucket Pipelines
// OIDC issuer; only the workspace segment varies, so it is bounded to a
// single path segment.
var bitbucketIssuerPath = regexp.MustCompile(`^/2\.0/workspaces/[^/]+/pipelines-config/identity/oidc$`)

// bitbucketIssuerMatch recognizes a Bitbucket Pipelines OIDC issuer. The
// issuer is per-workspace (its path names the workspace), so it cannot be a
// fixed allowlist entry; this pins the scheme and host — whatever it admits
// the verifier will fetch discovery from — and bounds the workspace to one
// path segment, leaving the actual trust to the signature and the repo-id
// match. No credentials, query, or fragment: a real issuer carries none,
// and admitting them would only widen what we fetch.
func bitbucketIssuerMatch(issuer string) bool {
	u, err := url.Parse(issuer)
	if err != nil {
		return false
	}
	return u.Scheme == "https" && u.Host == "api.bitbucket.org" &&
		u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		bitbucketIssuerPath.MatchString(u.Path)
}

// oidcForge names the forge a verified token's issuer belongs to, or "" for
// an issuer with no mapping (which the allowlist and matcher keep out). The
// GitLab set is per-instance — gitlab.com plus any operator-configured
// self-managed issuers — so it lives on the server, not in a constant.
func (s *Server) oidcForge(issuer string) string {
	switch {
	case issuer == gitHubActionsIssuer:
		return "github"
	case bitbucketIssuerMatch(issuer):
		return "bitbucket"
	case s.gitlabIssuers[issuer]:
		return "gitlab"
	}
	return ""
}

// authOIDC authenticates an upload carrying a forge OIDC token in the
// oidc_token form field, writing the error response itself; a false second
// return means the response is written. The body is already parsed by the
// caller. Rejections are explicit, like the fork-PR path: the uploader
// prints our reason into the CI log, and silent failure is the competitor
// behavior this feature exists to not have.
func (s *Server) authOIDC(w http.ResponseWriter, r *http.Request) (*store.Repo, bool) {
	ctx := r.Context()
	if s.oidc == nil {
		httpError(w, http.StatusForbidden, "OIDC uploads are not available: this server has no public URL configured")
		return nil, false
	}
	tok, err := s.oidc.Verify(ctx, r.FormValue("oidc_token"))
	switch {
	case err == nil:
	case errors.Is(err, oidc.ErrUnknownIssuer):
		httpError(w, http.StatusForbidden, "oidc_unknown_issuer: the OIDC token's issuer is not one this server trusts")
		return nil, false
	case errors.Is(err, oidc.ErrBadAudience):
		httpError(w, http.StatusForbidden, "oidc_bad_audience: the OIDC token was not minted for this server; set the token audience to %s", s.baseURL)
		return nil, false
	case errors.Is(err, oidc.ErrInvalidToken):
		httpError(w, http.StatusUnauthorized, "oidc_invalid_token: the OIDC token is not valid (%v)", err)
		return nil, false
	default:
		s.log.Error("oidc verification", "err", err)
		httpError(w, http.StatusBadGateway, "could not verify the OIDC token with the forge; retrying may help")
		return nil, false
	}

	switch s.oidcForge(tok.Issuer) {
	case "github":
		// GitHub names the repo by slug in the token itself.
		return s.oidcResolveBySlug(w, r, tok.Claim("repository"), "github")
	case "gitlab":
		// GitLab names the project by its full path — the slug.
		return s.oidcResolveBySlug(w, r, tok.Claim("project_path"), "gitlab")
	case "bitbucket":
		return s.oidcResolveBitbucket(w, r, tok)
	default:
		httpError(w, http.StatusForbidden, "oidc_invalid_token: unsupported OIDC issuer")
		return nil, false
	}
}

// oidcResolveBySlug maps a signed slug claim to a tracked repo and
// cross-checks the request's own repo field against it. The signed claim is
// authoritative — the form field, if sent, only has to agree.
func (s *Server) oidcResolveBySlug(w http.ResponseWriter, r *http.Request, slug, forgeName string) (*store.Repo, bool) {
	if slug == "" {
		httpError(w, http.StatusForbidden, "oidc_invalid_token: the OIDC token has no repository claim")
		return nil, false
	}
	repo, ok := s.oidcTrackedRepo(w, r, slug, forgeName)
	if !ok {
		return nil, false
	}
	if formRepo := r.FormValue("repo"); formRepo != "" && formRepo != repo.Slug {
		httpError(w, http.StatusForbidden, "oidc_repo_mismatch: the OIDC token is for %q, not %q", repo.Slug, formRepo)
		return nil, false
	}
	s.log.Info("oidc upload verified", "repo", repo.Slug, "forge", forgeName)
	return repo, true
}

// oidcResolveBitbucket maps a Bitbucket OIDC token to a tracked repo. The
// token names the repo only by an opaque UUID (in repositoryUuid, and in
// sub as "{repo-uuid}:{step-uuid}"), so the request's repo slug is the
// lookup key and the tracked repo's forge-reported UUID is the check: the
// slug resolves a candidate repo, and its live UUID — fetched through that
// repo's own workspace connection, not the caller's — must equal the signed
// one. A slug pointing at another repo therefore fails on the UUID, so the
// untrusted slug can never redirect the upload.
func (s *Server) oidcResolveBitbucket(w http.ResponseWriter, r *http.Request, tok *oidc.Token) (*store.Repo, bool) {
	ctx := r.Context()
	wantUUID := bitbucketRepoUUID(tok)
	if wantUUID == "" {
		httpError(w, http.StatusForbidden, "oidc_invalid_token: the OIDC token has no repository id")
		return nil, false
	}
	slug := r.FormValue("repo")
	if slug == "" {
		httpError(w, http.StatusBadRequest, "Bitbucket OIDC uploads require the repo field")
		return nil, false
	}
	repo, ok := s.oidcTrackedRepo(w, r, slug, "bitbucket")
	if !ok {
		return nil, false
	}

	// The UUID check below makes a live forge call through the repo's own
	// connection, so — like the fork-PR path (tokenless.go) — rate-limit it
	// per repo before making it: a valid token replayed with a victim's slug
	// must not be able to hammer that workspace's Bitbucket connection.
	if !s.tokenless.allow(repo.Slug, time.Now()) {
		httpError(w, http.StatusTooManyRequests, "OIDC upload rate limit reached for %s; try again later", repo.Slug)
		return nil, false
	}

	// Verify the slug→UUID binding through the repo's own workspace
	// connection: the caller does not choose which credentials answer this.
	fg, err := s.forges.For(ctx, repo)
	if err != nil {
		s.internalError(w, "resolving forge connection", err)
		return nil, false
	}
	if fg == nil {
		httpError(w, http.StatusForbidden, "OIDC uploads for %s need its workspace connected to Bitbucket; an admin can connect it from the workspace settings", ownerOf(repo.Slug))
		return nil, false
	}
	gotUUID, err := fg.GetRepoID(ctx, repo.Slug)
	if err != nil {
		s.log.Error("oidc bitbucket repo id", "repo", repo.Slug, "err", err)
		httpError(w, http.StatusBadGateway, "could not verify the repository identity with Bitbucket; retrying may help")
		return nil, false
	}
	if normalizeUUID(gotUUID) != normalizeUUID(wantUUID) {
		httpError(w, http.StatusForbidden, "oidc_repo_mismatch: the OIDC token's repository does not match %q", repo.Slug)
		return nil, false
	}
	s.log.Info("oidc upload verified", "repo", repo.Slug, "forge", "bitbucket")
	return repo, true
}

// oidcTrackedRepo resolves a slug to a repo tracked on the expected forge,
// writing the 404 itself when it is not.
func (s *Server) oidcTrackedRepo(w http.ResponseWriter, r *http.Request, slug, forgeName string) (*store.Repo, bool) {
	repo, err := s.store.RepoBySlug(r.Context(), slug)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "repo %q is not tracked on this server; OIDC uploads cannot register repos", slug)
		return nil, false
	}
	if err != nil {
		s.internalError(w, "looking up repo", err)
		return nil, false
	}
	if repo.Forge != forgeName {
		httpError(w, http.StatusNotFound, "repo %q is not tracked as a %s repo", slug, forgeName)
		return nil, false
	}
	return repo, true
}

// bitbucketRepoUUID reads the repository UUID a Bitbucket token names its
// repo by: the repositoryUuid claim, falling back to the "{repo}:{step}"
// sub. Returned as the token carries it (braces and all); the caller
// normalizes before comparing.
func bitbucketRepoUUID(tok *oidc.Token) string {
	if u := tok.Claim("repositoryUuid"); u != "" {
		return u
	}
	if sub := tok.Subject; sub != "" {
		id, _, _ := strings.Cut(sub, ":")
		return id
	}
	return ""
}

// normalizeUUID folds the incidental differences between how Bitbucket's
// REST resource and its OIDC claims spell the same UUID — surrounding braces
// and letter case — so the comparison is on identity, not spelling.
func normalizeUUID(s string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(s), "{}"))
}
