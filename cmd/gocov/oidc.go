package main

import (
	"encoding/json"
	"fmt"
	"io"
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
// present but the request failed — a real problem worth surfacing, though
// the caller still degrades rather than breaking the build.
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

	resp, err := doer.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("id-token endpoint returned %d", resp.StatusCode)
	}
	var out struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decoding id-token response: %w", err)
	}
	if out.Value == "" {
		return "", fmt.Errorf("id-token endpoint returned an empty token")
	}
	return out.Value, nil
}

// bitbucketOIDCToken returns the OIDC identity token Bitbucket Pipelines
// injects into a step that opted in with `oidc: true` (and named gocov in
// its `oidc.audiences`). Unlike GitHub there is no request to make — the
// token is handed to the step in an environment variable — so this is a
// read, not a mint. Empty outside such a step, so the caller falls through.
func bitbucketOIDCToken(env envFunc) string {
	return env("BITBUCKET_STEP_OIDC_TOKEN")
}

// gitlabOIDCToken returns the OIDC ID token a GitLab CI job mints through
// its id_tokens: block — which the gocov snippet names GOCOV_ID_TOKEN.
// GitLab, like Bitbucket, hands the token to the job in an environment
// variable, so this is a read, not a mint. Empty when not set.
func gitlabOIDCToken(env envFunc) string {
	return env("GOCOV_ID_TOKEN")
}

// envOIDCToken returns the OIDC identity token a forge hands the job through
// the environment (Bitbucket, GitLab), or "" when neither is present.
func envOIDCToken(env envFunc) string {
	if t := bitbucketOIDCToken(env); t != "" {
		return t
	}
	return gitlabOIDCToken(env)
}

// httpDoer is the slice of *http.Client the OIDC mint needs, kept as an
// interface so tests can stub the request.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// defaultHTTPDoer is the production httpDoer: a client with a short timeout,
// since the id-token endpoint is local to the runner.
var defaultHTTPDoer httpDoer = &http.Client{Timeout: 30 * time.Second}
