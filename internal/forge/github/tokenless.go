// Tokenless fork-PR upload verification (Şerit A): the server-side half
// of accepting an upload that carries no bearer token. A fork's
// pull_request workflow cannot hold secrets, so instead of a token the
// uploader claims "I am run N, attempt A, of a pull_request workflow on
// repo R, building PR P at head SHA H from fork F" — and this file checks
// every part of that claim against GitHub, authenticated as the
// workspace's App installation (5000 req/h, where Codecov's anonymous
// lookups drown at 60/h).

package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gocov/gocov/internal/forge/internal/rest"
)

// RunClaim is what a tokenless upload asserts about itself. RepoSlug is
// the upstream repo the upload targets (GITHUB_REPOSITORY); HeadRepo is
// the fork the PR head lives on; HeadSHA is the PR head commit — the
// event payload's head SHA, never GITHUB_SHA's throwaway merge commit.
type RunClaim struct {
	RepoSlug   string
	RunID      int64
	RunAttempt int64
	PRNumber   int64
	HeadSHA    string
	HeadRepo   string
}

// ClaimRejectedError is a definitive verification failure: GitHub was
// reached and the claim did not hold up. Reason is safe to show the
// uploader — the spec forbids silent rejection. Any other error from
// VerifyRunClaim is transient (network, GitHub 5xx) and worth retrying.
type ClaimRejectedError struct {
	Reason string
}

func (e *ClaimRejectedError) Error() string {
	return "github: tokenless claim rejected: " + e.Reason
}

func reject(format string, args ...any) error {
	return &ClaimRejectedError{Reason: fmt.Sprintf(format, args...)}
}

// VerifyRunClaim checks a tokenless upload's claim, authenticated as the
// installation: the repo is public, the workflow run is real, currently
// running, a pull_request event of that very repo at the claimed head and
// attempt, and the PR is open with the claimed head SHA and fork. A
// *ClaimRejectedError means the claim is definitively bad; other errors
// are transient. What survives all checks can still be a fabricated
// report raced into a real run's window — an accepted risk the caller
// bounds with single-acceptance and rate limits, and marks as unverified.
func (a *App) VerifyRunClaim(ctx context.Context, installationID int64, claim RunClaim) error {
	// Repo public? Tokenless is public-only, without exception: the
	// verification leans on world-readable run metadata, and a private
	// repo's CI can hold a real token.
	var repo struct {
		Private bool `json:"private"`
	}
	notFound, err := a.getAsInstallation(ctx, installationID, "/repos/"+claim.RepoSlug, &repo)
	if err != nil {
		return err
	}
	if notFound {
		return reject("the installation cannot see %s", claim.RepoSlug)
	}
	if repo.Private {
		return reject("%s is private; tokenless uploads work on public repos only", claim.RepoSlug)
	}

	// Run real? The run is fetched through the claimed repo's own path, and
	// its repository echoed back must match — that closes replaying another
	// repo's run id. Only a run still in progress may upload: a completed
	// run's id is public forever, an in-progress one is a minutes-wide window.
	var run struct {
		Status     string `json:"status"`
		Event      string `json:"event"`
		HeadSHA    string `json:"head_sha"`
		RunAttempt int64  `json:"run_attempt"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	notFound, err = a.getAsInstallation(ctx, installationID,
		fmt.Sprintf("/repos/%s/actions/runs/%d", claim.RepoSlug, claim.RunID), &run)
	if err != nil {
		return err
	}
	switch {
	case notFound:
		return reject("workflow run %d not found on %s", claim.RunID, claim.RepoSlug)
	case !strings.EqualFold(run.Repository.FullName, claim.RepoSlug):
		return reject("workflow run %d belongs to another repository", claim.RunID)
	case run.Event != "pull_request":
		return reject("workflow run %d is a %q run; only pull_request runs may upload tokenless", claim.RunID, run.Event)
	case run.Status != "in_progress":
		return reject("workflow run %d is %s; only a run still in progress may upload", claim.RunID, run.Status)
	case !strings.EqualFold(run.HeadSHA, claim.HeadSHA):
		return reject("workflow run %d is not building commit %s", claim.RunID, claim.HeadSHA)
	case run.RunAttempt != claim.RunAttempt:
		return reject("workflow run %d is on attempt %d, not %d", claim.RunID, run.RunAttempt, claim.RunAttempt)
	}

	// PR real? Open, at the claimed head, from the claimed fork.
	var pr struct {
		State string `json:"state"`
		Head  struct {
			SHA  string `json:"sha"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
	}
	notFound, err = a.getAsInstallation(ctx, installationID,
		fmt.Sprintf("/repos/%s/pulls/%d", claim.RepoSlug, claim.PRNumber), &pr)
	if err != nil {
		return err
	}
	switch {
	case notFound:
		return reject("pull request #%d not found on %s", claim.PRNumber, claim.RepoSlug)
	case pr.State != "open":
		return reject("pull request #%d is %s, not open", claim.PRNumber, pr.State)
	case !strings.EqualFold(pr.Head.SHA, claim.HeadSHA):
		return reject("pull request #%d's head is not %s", claim.PRNumber, claim.HeadSHA)
	case !strings.EqualFold(pr.Head.Repo.FullName, claim.HeadRepo):
		return reject("pull request #%d's head is not on %s", claim.PRNumber, claim.HeadRepo)
	}
	return nil
}

// getAsInstallation performs a GET authenticated as the installation.
// notFound reports a 404 answer — for verification that is a claim
// verdict, not an error. A mint failure surfaces as the usual
// ErrCredentialsRevoked-wrapped error.
func (a *App) getAsInstallation(ctx context.Context, installationID int64, path string, out any) (notFound bool, err error) {
	token, _, err := a.installationToken(ctx, installationID)
	if err != nil {
		return false, err
	}
	err = a.api(token).Get(ctx, path, out)
	if rest.Status(err) == http.StatusNotFound {
		return true, nil
	}
	return false, err
}
