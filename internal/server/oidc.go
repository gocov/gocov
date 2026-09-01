// Tokenless OIDC uploads: authenticating an upload that carries no bearer
// token and no fork-PR run claim, but a forge-minted OIDC identity token.
// A repo owner's own CI (push, same-repo PR) asks the forge for a
// short-lived JWT scoped to gocov's audience and sends it in place of the
// pasted GOCOV_TOKEN; this file verifies that JWT (internal/oidc has the
// protocol), maps its repository claim to a tracked repo, and cross-checks
// the upload's own repo field against it. Unlike a fork-PR tokenless upload
// (tokenless.go), the result is a fully verified upload — the forge signed
// the token, so there is no "unverified" badge.
package server

import (
	"errors"
	"net/http"

	"github.com/gocov/gocov/internal/oidc"
	"github.com/gocov/gocov/internal/store"
)

// gitHubActionsIssuer is the GitHub Actions OIDC issuer. Its identity tokens
// carry a "repository" claim (owner/name) that names the repo a run belongs
// to — the claim we map to a tracked repo.
const gitHubActionsIssuer = "https://token.actions.githubusercontent.com"

// defaultOIDCIssuers is the allowlist of forge OIDC issuers gocov trusts.
// GitHub only for now; GitLab and Bitbucket issuers (and operator-configured
// self-hosted GitLab) join it as their forge support lands.
func defaultOIDCIssuers() []string {
	return []string{gitHubActionsIssuer}
}

// oidcRepoIdentity reads the repository-identity claim from a verified token
// and reports which forge it belongs to. Each forge names the claim
// differently; ok is false for an issuer we recognize but have no mapping
// for (which cannot happen while the allowlist and this switch agree).
func oidcRepoIdentity(tok *oidc.Token) (slug, forge string, ok bool) {
	switch tok.Issuer {
	case gitHubActionsIssuer:
		return tok.Claim("repository"), "github", true
	}
	return "", "", false
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

	slug, forgeName, ok := oidcRepoIdentity(tok)
	if !ok || slug == "" {
		httpError(w, http.StatusForbidden, "oidc_invalid_token: the OIDC token has no repository claim")
		return nil, false
	}

	repo, err := s.store.RepoBySlug(ctx, slug)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "repo %q is not tracked on this server; OIDC uploads cannot register repos", slug)
		return nil, false
	}
	if err != nil {
		s.internalError(w, "looking up repo", err)
		return nil, false
	}
	if repo.Forge != forgeName {
		// The issuer's forge and the tracked repo's forge disagree — the slug
		// belongs to a repo on another forge. Refuse rather than report to it.
		httpError(w, http.StatusNotFound, "repo %q is not tracked as a %s repo", slug, forgeName)
		return nil, false
	}

	// Cross-check the upload's own repo field against the signed claim, so a
	// repo cannot use its OIDC token to push a report onto another repo. The
	// signed claim wins; the form field only has to agree with it.
	if formRepo := r.FormValue("repo"); formRepo != "" && formRepo != repo.Slug {
		httpError(w, http.StatusForbidden, "oidc_repo_mismatch: the OIDC token is for %q, not %q", repo.Slug, formRepo)
		return nil, false
	}

	s.log.Info("oidc upload verified", "repo", repo.Slug, "issuer", tok.Issuer)
	return repo, true
}
