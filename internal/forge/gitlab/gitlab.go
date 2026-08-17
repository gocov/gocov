// Package gitlab implements forge.Forge for GitLab (gitlab.com; a
// self-managed instance works through BaseURL but is not yet officially
// supported).
package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/forge"
)

// DefaultBaseURL is the GitLab REST API root. Kept a field on Client so
// tests and self-managed instances can point elsewhere.
const DefaultBaseURL = "https://gitlab.com/api/v4"

// Client implements forge.Forge against the GitLab REST API using a
// personal, project or group access token (scope "api") for
// authentication.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// Factory builds a Client from repo credentials. Required key: "token";
// optional "base_url" points at a self-managed instance.
func Factory(creds map[string]string) (forge.Forge, error) {
	token := creds["token"]
	if token == "" {
		return nil, fmt.Errorf("gitlab: credentials must include token")
	}
	baseURL := creds["base_url"]
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:    baseURL,
		Token:      token,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// authorize sets the request's auth header. Bearer works for personal,
// project and group access tokens alike (and for OAuth tokens, unlike
// the PRIVATE-TOKEN header).
func (c *Client) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.Token)
}

// projectID is the URL path form of a project: the full namespace path,
// URL-encoded into a single segment ("a/b/c" → "a%2Fb%2Fc"). The GitLab
// API's best-known trap — a bare slash in the path 404s.
func projectID(slug string) string {
	return url.PathEscape(slug)
}

var stateNames = map[string]string{
	forge.StateSuccessful: "success",
	forge.StateFailed:     "failed",
	forge.StateInProgress: "pending",
}

// PostBuildStatus writes a commit status via
// POST /projects/{id}/statuses/{sha}. The status name is the short Name
// ("gocov") — the string merge-request status checks match on — with Key
// as the fallback.
func (c *Client) PostBuildStatus(ctx context.Context, repoSlug, commitSHA string, status forge.BuildStatus) error {
	state, ok := stateNames[status.State]
	if !ok {
		return fmt.Errorf("gitlab: unknown build status state %q", status.State)
	}
	name := status.Name
	if name == "" {
		name = status.Key
	}
	body := map[string]string{
		"state":       state,
		"name":        name,
		"description": status.Description,
	}
	if status.URL != "" {
		body["target_url"] = status.URL
	}
	path := fmt.Sprintf("/projects/%s/statuses/%s", projectID(repoSlug), url.PathEscape(commitSHA))
	err := c.send(ctx, http.MethodPost, path, body)
	// Re-posting the state a commit already has (a re-upload merging
	// another report into the same commit) is a 400 "Cannot transition
	// status" — the status is already what we want it to be, not a failure.
	var ae *apiError
	if errors.As(err, &ae) && ae.status == http.StatusBadRequest &&
		strings.Contains(ae.msg, "Cannot transition status") {
		return nil
	}
	return err
}

// PostPRComment adds a note via
// POST /projects/{id}/merge_requests/{iid}/notes.
func (c *Client) PostPRComment(ctx context.Context, repoSlug, prID, body string) error {
	path := fmt.Sprintf("/projects/%s/merge_requests/%s/notes", projectID(repoSlug), url.PathEscape(prID))
	return c.send(ctx, http.MethodPost, path, map[string]string{"body": body})
}

// maxNotePages bounds pagination when searching for an earlier note.
const maxNotePages = 10

// FindPRComment returns the id of the newest non-system MR note whose
// body starts with prefix. Matching is marker-based without an author
// check: GitLab rejects edits of notes the token does not own, so a
// foreign look-alike note cannot durably capture the slot — the caller
// falls back to posting a fresh note. Notes are listed oldest first so
// all pages are walked and the last match wins.
func (c *Client) FindPRComment(ctx context.Context, repoSlug, prID, prefix string) (string, error) {
	next := fmt.Sprintf("%s/projects/%s/merge_requests/%s/notes?order_by=created_at&sort=asc&per_page=100",
		c.BaseURL, projectID(repoSlug), url.PathEscape(prID))
	found := ""
	for page := 0; next != "" && page < maxNotePages; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return "", err
		}
		c.authorize(req)
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("gitlab: %w", err)
		}
		if resp.StatusCode >= 300 {
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return "", fmt.Errorf("gitlab: listing MR notes returned %d: %s", resp.StatusCode, msg)
		}
		var notes []struct {
			ID     int64  `json:"id"`
			Body   string `json:"body"`
			System bool   `json:"system"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&notes)
		link := resp.Header.Get("Link")
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("gitlab: decoding MR notes: %w", err)
		}
		for _, n := range notes {
			if !n.System && strings.HasPrefix(n.Body, prefix) {
				found = strconv.FormatInt(n.ID, 10)
			}
		}
		next = nextLink(link)
	}
	return found, nil
}

// nextLink extracts the rel="next" URL from a Link response header, or ""
// when there is no next page.
func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		u, rel, ok := strings.Cut(part, ";")
		if !ok || !strings.Contains(rel, `rel="next"`) {
			continue
		}
		u = strings.TrimSpace(u)
		if strings.HasPrefix(u, "<") && strings.HasSuffix(u, ">") {
			return u[1 : len(u)-1]
		}
	}
	return ""
}

// UpdatePRComment replaces a note's body via
// PUT /projects/{id}/merge_requests/{iid}/notes/{note_id}.
func (c *Client) UpdatePRComment(ctx context.Context, repoSlug, prID, commentID, body string) error {
	path := fmt.Sprintf("/projects/%s/merge_requests/%s/notes/%s",
		projectID(repoSlug), url.PathEscape(prID), url.PathEscape(commentID))
	return c.send(ctx, http.MethodPut, path, map[string]string{"body": body})
}

// maxDiffBytes bounds MR diffs; larger diffs error instead of truncating.
const maxDiffBytes = 32 << 20

// GetPRDiff fetches the MR's changes via
// GET /projects/{id}/merge_requests/{iid}/changes and reassembles them
// into a unified diff (the changes API returns per-file hunks without
// ---/+++ headers). A response flagged overflow would be an incomplete
// diff and silently wrong coverage numbers, so it errors instead.
func (c *Client) GetPRDiff(ctx context.Context, repoSlug, prID string) (string, error) {
	path := fmt.Sprintf("/projects/%s/merge_requests/%s/changes", projectID(repoSlug), url.PathEscape(prID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return "", err
	}
	c.authorize(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gitlab: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("gitlab: %s returned %d: %s", path, resp.StatusCode, msg)
	}
	var body struct {
		Overflow bool `json:"overflow"`
		Changes  []struct {
			OldPath     string `json:"old_path"`
			NewPath     string `json:"new_path"`
			DeletedFile bool   `json:"deleted_file"`
			Diff        string `json:"diff"`
		} `json:"changes"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDiffBytes)).Decode(&body); err != nil {
		return "", fmt.Errorf("gitlab: decoding MR changes: %w", err)
	}
	if body.Overflow {
		return "", fmt.Errorf("gitlab: MR %s diff exceeds the API's limits (overflow)", prID)
	}
	var sb strings.Builder
	for _, ch := range body.Changes {
		if ch.DeletedFile {
			continue
		}
		sb.WriteString("--- a/")
		sb.WriteString(ch.OldPath)
		sb.WriteString("\n+++ b/")
		sb.WriteString(ch.NewPath)
		sb.WriteString("\n")
		sb.WriteString(ch.Diff)
		if ch.Diff != "" && !strings.HasSuffix(ch.Diff, "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

// GetDefaultBranch reads the project's default branch via
// GET /projects/{id}.
func (c *Client) GetDefaultBranch(ctx context.Context, repoSlug string) (string, error) {
	path := "/projects/" + projectID(repoSlug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return "", err
	}
	c.authorize(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gitlab: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%w: %s", forge.ErrRepoNotFound, repoSlug)
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("gitlab: %s returned %d: %s", path, resp.StatusCode, msg)
	}
	var body struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("gitlab: decoding project: %w", err)
	}
	if body.DefaultBranch == "" {
		return "", fmt.Errorf("gitlab: project %s has no default branch", repoSlug)
	}
	return body.DefaultBranch, nil
}

// maxFileBytes bounds source files fetched for the source view.
const maxFileBytes = 2 << 20

// GetFileContent reads a file at a commit via
// GET /projects/{id}/repository/files/{path}/raw?ref={sha}. The file path
// is URL-encoded into a single segment, same as the project path.
func (c *Client) GetFileContent(ctx context.Context, repoSlug, commitSHA, path string) ([]byte, error) {
	reqURL := fmt.Sprintf("%s/projects/%s/repository/files/%s/raw?ref=%s",
		c.BaseURL, projectID(repoSlug), url.PathEscape(path), url.QueryEscape(commitSHA))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	c.authorize(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s at %s", forge.ErrRepoNotFound, path, commitSHA)
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("gitlab: reading %s returned %d: %s", path, resp.StatusCode, msg)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("gitlab: reading %s: %w", path, err)
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("gitlab: %s is larger than %d MiB", path, maxFileBytes>>20)
	}
	return data, nil
}

// PublishReport is not supported: GitLab's only external line-annotation
// surface is MR diff discussions, which would open one thread per
// uncovered line — deliberately not done (noise); the MR note's diff
// coverage table carries the same information. The upload flow treats
// the sentinel as "skipped".
func (c *Client) PublishReport(ctx context.Context, repoSlug, commitSHA string, report forge.Report, annotations []forge.Annotation) error {
	return fmt.Errorf("%w: GitLab has no commit report surface; the MR comment carries the diff coverage table", forge.ErrNotImplemented)
}

func (c *Client) send(ctx context.Context, method, path string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("gitlab: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &apiError{
			status: resp.StatusCode,
			msg:    fmt.Sprintf("gitlab: %s returned %d: %s", path, resp.StatusCode, msg),
		}
	}
	return nil
}

// apiError carries the HTTP status of a failed GitLab call so callers
// can react to specific rejections.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

var _ forge.Forge = (*Client)(nil)
