package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// mintGitHubOIDC asks GitHub Actions for an OIDC identity token bound to the
// gocov server's audience, so an upload can prove which repository it came
// from without a pasted GOCOV_TOKEN. GitHub injects the request URL and a
// bearer token into the job's environment only when the workflow grants
// `permissions: id-token: write`; without them (no permission, a fork PR, or
// not GitHub Actions at all) this returns ("", nil) and the caller falls
// through to the next auth mode. A non-empty error means the permission was
// present but the request failed — a real problem worth surfacing; the
// caller reports it and falls through, so the upload may still error like a
// missing token (e.g. a push build with no other auth mode) rather than
// always degrading to a warning.
//
// The token never touches argv: it is read from the environment and passed
// on as a request field, the same discipline the bearer token follows.
func mintGitHubOIDC(env envFunc, doer httpDoer, audience string) (string, error) {
	reqURL := env("ACTIONS_ID_TOKEN_REQUEST_URL")
	reqToken := env("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	if reqURL == "" || reqToken == "" {
		return "", nil
	}

	sep := "?"
	if strings.Contains(reqURL, "?") {
		sep = "&"
	}
	full := reqURL + sep + "audience=" + url.QueryEscape(strings.TrimRight(audience, "/"))
	req, err := http.NewRequest(http.MethodGet, full, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+reqToken)
	req.Header.Set("Accept", "application/json")

	status, body, err := send(doer, req)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("id-token endpoint returned %d", status)
	}
	var out struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decoding id-token response: %w", err)
	}
	if out.Value == "" {
		return "", errors.New("id-token endpoint returned an empty token")
	}
	return out.Value, nil
}

// envOIDCToken returns the OIDC identity token a forge hands the job through
// the environment, or "" when none is present so the caller falls through.
// Unlike GitHub there is no request to make — this is a read, not a mint.
// Bitbucket Pipelines injects BITBUCKET_STEP_OIDC_TOKEN into a step that
// opted in with `oidc: true` (and named gocov in its `oidc.audiences`);
// GitLab CI mints one through the job's id_tokens: block, which the gocov
// snippet names GOCOV_ID_TOKEN.
func envOIDCToken(env envFunc) string {
	return cmp.Or(env("BITBUCKET_STEP_OIDC_TOKEN"), env("GOCOV_ID_TOKEN"))
}

// defaultHTTPDoer is the production httpDoer for the OIDC mint: a client
// with a short timeout, since the id-token endpoint is local to the runner.
var defaultHTTPDoer httpDoer = &http.Client{Timeout: 30 * time.Second}
