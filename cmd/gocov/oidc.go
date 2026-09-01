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

// httpDoer is the slice of *http.Client the OIDC mint needs, kept as an
// interface so tests can stub the request.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// defaultHTTPDoer is the production httpDoer: a client with a short timeout,
// since the id-token endpoint is local to the runner.
var defaultHTTPDoer httpDoer = &http.Client{Timeout: 30 * time.Second}
