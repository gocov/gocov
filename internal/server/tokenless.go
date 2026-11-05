// Tokenless fork-PR uploads (Şerit A): authenticating an upload that
// carries no bearer token. A fork's pull_request workflow has no secrets,
// so instead of a token the request claims which workflow run it is, and
// the claim is verified against GitHub through the workspace's App
// installation (forge/github/tokenless.go has the protocol). This file is
// the transport half: reading the claim out of the form, the checks that
// don't need GitHub, and the per-repo rate limit that bounds abuse of the
// ones that do.

package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/forge/github"
	"github.com/gocov/gocov/internal/store"
)

// maxTokenlessPerRepoHour caps tokenless verification attempts per repo
// per hour. Each attempt can cost up to three GitHub API calls from the
// installation's 5000/h budget; the cap keeps a flood from a public
// repo's spectators well below that while leaving room for real matrix
// builds (the parts-per-commit cap is 50). The same counter, keyed apart
// per forge, bounds the Bitbucket OIDC repo-identity checks (oidc.go,
// reportapi.go).
//
// Known limit: the server cannot tell a genuine claim from a made-up one
// without asking GitHub, so an anonymous caller sending a new, invented
// run id every time still uses up a repo's attempts for the hour and
// delays genuine fork-PR uploads until the window resets. What keeps that
// small: a claim already decided is answered from claimVerdicts without
// an attempt (so neither replays nor a real run's further parts count),
// and checks that cost nothing run before an attempt is charged.
const maxTokenlessPerRepoHour = 60

// tokenlessLimiter is a fixed-window counter per forge and repo. One
// instance lives on the Server; a multi-instance deployment would
// rate-limit per instance, which only loosens the bound, never blocks a
// real upload.
type tokenlessLimiter struct {
	mu     sync.Mutex
	window time.Time
	counts map[string]int
}

func newTokenlessLimiter() *tokenlessLimiter {
	return &tokenlessLimiter{counts: map[string]int{}}
}

// allow consumes one attempt for the forge's repo and reports whether it
// fit the current window. The forge is part of the key: a GitHub and a
// Bitbucket repo of the same name are different repos.
func (l *tokenlessLimiter) allow(forgeName, slug string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.window) >= time.Hour {
		l.window = now
		clear(l.counts)
	}
	key := forgeName + ":" + slug
	if l.counts[key] >= maxTokenlessPerRepoHour {
		return false
	}
	l.counts[key]++
	return true
}

// claimVerdictTTL is how long a decided claim is answered from memory. A
// rejection is GitHub's definitive word on that exact claim; an acceptance
// covers a run that is in progress, which a fork PR's parts arrive within.
// Short either way: a run can finish, and a PR can move on.
const claimVerdictTTL = 5 * time.Minute

// maxClaimVerdicts bounds the cache; past it, it starts over.
const maxClaimVerdicts = 10_000

// claimVerdicts remembers GitHub's answer to each exact claim for a few
// minutes, so that the same claim sent again — a replay, or a real run's
// next part — costs neither a verification attempt nor an API call. It is
// keyed by the whole claim, never by the run id alone: a rejection for a
// claim naming a real run with a wrong SHA must not shut that run out.
type claimVerdicts struct {
	mu sync.Mutex
	m  map[github.RunClaim]claimVerdict
}

type claimVerdict struct {
	rejected string // GitHub's reason; empty when the claim held up
	until    time.Time
}

func newClaimVerdicts() *claimVerdicts {
	return &claimVerdicts{m: map[github.RunClaim]claimVerdict{}}
}

// get returns the remembered verdict for the claim, if one is still fresh.
func (c *claimVerdicts) get(claim github.RunClaim, now time.Time) (claimVerdict, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[claim]
	if !ok || now.After(v.until) {
		return claimVerdict{}, false
	}
	return v, true
}

// put remembers a definitive verdict for the claim.
func (c *claimVerdicts) put(claim github.RunClaim, rejected string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= maxClaimVerdicts {
		clear(c.m)
	}
	c.m[claim] = claimVerdict{rejected: rejected, until: now.Add(claimVerdictTTL)}
}

// tokenlessClaim is the request's claim after local validation, plus what
// the dedupe needs later in the flow.
type tokenlessClaim struct {
	runID      int64
	runAttempt int64
}

// authTokenless authenticates an upload request that arrived without a
// bearer token, writing the error response itself; a false last return
// means the response is written. It parses the multipart body — tokenless
// requests have nothing to authenticate before it — validates the claim
// locally, and has the claim verified against GitHub through the
// workspace's installation. Rejections are deliberately explicit: the
// uploader prints our reason into the CI log, and silent failure is the
// competitor behavior this feature exists to not have.
func (s *Server) authTokenless(w http.ResponseWriter, r *http.Request) (*tokenlessClaim, *store.Repo, bool) {
	ctx := r.Context()
	if !s.parseUploadBody(w, r) {
		return nil, nil, false
	}
	if r.FormValue("run_id") == "" {
		// Not a tokenless attempt at all — the historical answer for a
		// request with no credentials.
		httpError(w, http.StatusUnauthorized, "missing bearer token")
		return nil, nil, false
	}
	if s.forges.GitHubApp == nil {
		httpError(w, http.StatusForbidden, "tokenless uploads are not available: this server has no GitHub App configured")
		return nil, nil, false
	}

	claim := github.RunClaim{
		RepoSlug: r.FormValue("repo"),
		HeadSHA:  r.FormValue("commit"),
		HeadRepo: r.FormValue("head_repo"),
	}
	var parseErr string
	switch {
	case claim.RepoSlug == "":
		parseErr = "tokenless uploads require the repo field"
	case claim.HeadSHA == "" || !commitRe.MatchString(claim.HeadSHA):
		parseErr = "tokenless uploads require the commit field (the PR head SHA)"
	case claim.HeadRepo == "":
		parseErr = "tokenless uploads require the head_repo field (the fork the PR head is on)"
	}
	if parseErr == "" {
		var err error
		if claim.RunID, err = positiveInt(r.FormValue("run_id")); err != nil {
			parseErr = "invalid run_id: want the workflow run's numeric id"
		} else if claim.RunAttempt, err = positiveInt(r.FormValue("run_attempt")); err != nil {
			parseErr = "invalid run_attempt: want the workflow run's numeric attempt"
		} else if claim.PRNumber, err = positiveInt(r.FormValue("pr_id")); err != nil {
			parseErr = "tokenless uploads require pr_id (the pull request number)"
		}
	}
	if parseErr != "" {
		httpError(w, http.StatusBadRequest, "%s", parseErr)
		return nil, nil, false
	}

	// The repo must already be tracked: tokenless callers are anonymous
	// and may not register anything.
	repo, err := s.store.RepoBySlug(ctx, "github", claim.RepoSlug)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "repo %q is not tracked on this server; tokenless uploads cannot register repos", claim.RepoSlug)
		return nil, nil, false
	}
	if err != nil {
		s.internalError(w, "looking up repo", err)
		return nil, nil, false
	}
	claim.RepoSlug = repo.Slug // canonical casing for the API paths
	if repo.Forge != "github" {
		httpError(w, http.StatusForbidden, "tokenless uploads are GitHub-only; %s is on %s", repo.Slug, repo.Forge)
		return nil, nil, false
	}
	// Everything that costs nothing comes before an attempt is charged.
	ws := s.forges.WorkspaceFor(ctx, repo.Slug, "github")
	if ws == nil || ws.GitHubInstallationID == 0 {
		httpError(w, http.StatusForbidden, "tokenless uploads need the gocov GitHub App installed on %s; a workspace owner can connect it from the workspace settings", ownerOf(repo.Slug))
		return nil, nil, false
	}
	now := time.Now()
	if v, ok := s.claimVerdicts.get(claim, now); ok {
		// GitHub already answered this exact claim: answer the same,
		// without an attempt or an API call.
		if v.rejected != "" {
			httpError(w, http.StatusForbidden, "tokenless upload rejected: %s", v.rejected)
			return nil, nil, false
		}
		return &tokenlessClaim{runID: claim.RunID, runAttempt: claim.RunAttempt}, repo, true
	}
	if !s.tokenless.allow("github", repo.Slug, now) {
		httpError(w, http.StatusTooManyRequests, "tokenless upload rate limit reached for %s; try again later", repo.Slug)
		return nil, nil, false
	}

	err = s.forges.VerifyGitHubRunClaim(ctx, ws, claim)
	switch rejected, definitive := errors.AsType[*github.ClaimRejectedError](err); {
	case err == nil:
		s.claimVerdicts.put(claim, "", now)
	case definitive:
		s.claimVerdicts.put(claim, rejected.Reason, now)
		s.log.Warn("tokenless upload rejected", "repo", repo.Slug, "run", claim.RunID, "reason", rejected.Reason)
		httpError(w, http.StatusForbidden, "tokenless upload rejected: %s", rejected.Reason)
		return nil, nil, false
	case errors.Is(err, forge.ErrCredentialsRevoked):
		httpError(w, http.StatusForbidden, "the gocov GitHub App installation for %s is no longer valid; a workspace owner needs to reconnect it", ws.Prefix)
		return nil, nil, false
	default:
		s.log.Error("tokenless verification", "repo", repo.Slug, "run", claim.RunID, "err", err)
		httpError(w, http.StatusBadGateway, "could not verify the workflow run with GitHub; retrying may help")
		return nil, nil, false
	}
	s.log.Info("tokenless upload verified", "repo", repo.Slug, "run", claim.RunID, "attempt", claim.RunAttempt, "pr", claim.PRNumber)
	return &tokenlessClaim{runID: claim.RunID, runAttempt: claim.RunAttempt}, repo, true
}

// positiveInt parses a required positive integer form value.
func positiveInt(s string) (int64, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, errors.New("not positive")
	}
	return n, nil
}

// ownerOf returns the workspace half of a repo slug.
func ownerOf(slug string) string {
	owner, _, _ := strings.Cut(slug, "/")
	return owner
}
