// Package gitlab implements forge.Forge for GitLab (gitlab.com; a
// self-managed instance works through BaseURL but is not yet officially
// supported).
package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/rest"
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

// authorize sets the request's auth header. Bearer works for personal,
// project and group access tokens alike (and for OAuth tokens, unlike
// the PRIVATE-TOKEN header).
func (c *Client) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.Token)
}

// api is the request plumbing, bound to this client's credentials.
func (c *Client) api() *rest.Client {
	return &rest.Client{Name: "gitlab", BaseURL: c.BaseURL, HTTPClient: c.HTTPClient, Authorize: c.authorize}
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
	err := c.api().Send(ctx, http.MethodPost, path, body)
	// Re-posting the state a commit already has (a re-upload merging
	// another report into the same commit) is a 400 "Cannot transition
	// status" — the status is already what we want it to be, not a failure.
	if e, ok := errors.AsType[*rest.Error](err); ok && e.Status == http.StatusBadRequest &&
		strings.Contains(e.Body, "Cannot transition status") {
		return nil
	}
	return err
}

// PostPRComment adds a note via
// POST /projects/{id}/merge_requests/{iid}/notes.
func (c *Client) PostPRComment(ctx context.Context, repoSlug, prID, body string) error {
	path := fmt.Sprintf("/projects/%s/merge_requests/%s/notes", projectID(repoSlug), url.PathEscape(prID))
	return c.api().Send(ctx, http.MethodPost, path, map[string]string{"body": body})
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
		var notes []struct {
			ID     int64  `json:"id"`
			Body   string `json:"body"`
			System bool   `json:"system"`
		}
		var err error
		if next, err = c.api().GetPage(ctx, next, &notes); err != nil {
			return "", err
		}
		for _, n := range notes {
			if !n.System && strings.HasPrefix(n.Body, prefix) {
				found = strconv.FormatInt(n.ID, 10)
			}
		}
	}
	if next != "" {
		// Beyond the cap an existing marker note can go unseen, making
		// every upload post a fresh comment — pathological (1000+ notes on
		// one MR) but worth a trace when it happens.
		slog.Warn("gitlab: MR note search stopped at page cap; an existing gocov comment may be missed",
			"repo", repoSlug, "mr", prID, "pages", maxNotePages)
	}
	return found, nil
}

// UpdatePRComment replaces a note's body via
// PUT /projects/{id}/merge_requests/{iid}/notes/{note_id}.
func (c *Client) UpdatePRComment(ctx context.Context, repoSlug, prID, commentID, body string) error {
	path := fmt.Sprintf("/projects/%s/merge_requests/%s/notes/%s",
		projectID(repoSlug), url.PathEscape(prID), url.PathEscape(commentID))
	return c.api().Send(ctx, http.MethodPut, path, map[string]string{"body": body})
}

// maxDiffBytes bounds MR diffs; larger diffs error instead of truncating.
const maxDiffBytes = 32 << 20

// GetPRDiff fetches the MR's changes via
// GET /projects/{id}/merge_requests/{iid}/changes and reassembles them
// into a unified diff (the changes API returns per-file hunks without
// ---/+++ headers). A response flagged overflow would be an incomplete
// diff and silently wrong coverage numbers, so it errors instead.
// GitLab has deprecated /changes in favor of the paginated /diffs
// endpoint; it still serves API v4, and switching to /diffs (which also
// lifts the overflow ceiling) is planned as a P1 follow-up.
func (c *Client) GetPRDiff(ctx context.Context, repoSlug, prID string) (string, error) {
	path := fmt.Sprintf("/projects/%s/merge_requests/%s/changes", projectID(repoSlug), url.PathEscape(prID))
	// Read through Do rather than Get: the changes document is a diff
	// wrapped in JSON, so it gets the diff's size allowance.
	resp, err := c.api().Do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
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

// fetchProject GETs the project resource (GET /projects/{id}) and decodes
// it into out — the request/status/decode plumbing GetDefaultBranch and
// GetRepoVisibility share.
func (c *Client) fetchProject(ctx context.Context, repoSlug string, out any) error {
	err := c.api().Get(ctx, "/projects/"+projectID(repoSlug), out)
	if rest.Status(err) == http.StatusNotFound {
		return fmt.Errorf("%w: %s", forge.ErrRepoNotFound, repoSlug)
	}
	return err
}

// GetDefaultBranch reads the project's default branch via
// GET /projects/{id}.
func (c *Client) GetDefaultBranch(ctx context.Context, repoSlug string) (string, error) {
	var body struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.fetchProject(ctx, repoSlug, &body); err != nil {
		return "", err
	}
	if body.DefaultBranch == "" {
		return "", fmt.Errorf("gitlab: project %s has no default branch", repoSlug)
	}
	return body.DefaultBranch, nil
}

// GetRepoVisibility reads the project's visibility via GET /projects/{id}.
// GitLab's "internal" (any signed-in GitLab account) is not world-readable,
// so it maps to private.
func (c *Client) GetRepoVisibility(ctx context.Context, repoSlug string) (string, error) {
	var body struct {
		Visibility string `json:"visibility"`
	}
	if err := c.fetchProject(ctx, repoSlug, &body); err != nil {
		return "", err
	}
	if body.Visibility == "public" {
		return forge.VisibilityPublic, nil
	}
	return forge.VisibilityPrivate, nil
}

// GetRepoID is not needed on GitLab: its CI OIDC tokens name the project by
// path (the "project_path" claim), so there is no opaque id to resolve.
func (c *Client) GetRepoID(ctx context.Context, repoSlug string) (string, error) {
	return "", forge.ErrNotImplemented
}

// maxFileBytes bounds source files fetched for the source view.
const maxFileBytes = 2 << 20

// GetFileContent reads a file at a commit via
// GET /projects/{id}/repository/files/{path}/raw?ref={sha}. The file path
// is URL-encoded into a single segment, same as the project path.
func (c *Client) GetFileContent(ctx context.Context, repoSlug, commitSHA, path string) ([]byte, error) {
	reqPath := fmt.Sprintf("/projects/%s/repository/files/%s/raw?ref=%s",
		projectID(repoSlug), url.PathEscape(path), url.QueryEscape(commitSHA))
	data, err := c.api().GetBytes(ctx, reqPath, "", maxFileBytes)
	if rest.Status(err) == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s at %s", forge.ErrRepoNotFound, path, commitSHA)
	}
	return data, err
}

// PublishReport is not supported: GitLab's only external line-annotation
// surface is MR diff discussions, which would open one thread per
// uncovered line — deliberately not done (noise); the MR note's diff
// coverage table carries the same information. The upload flow treats
// the sentinel as "skipped".
func (c *Client) PublishReport(ctx context.Context, repoSlug, commitSHA string, report forge.Report, annotations []forge.Annotation) error {
	return fmt.Errorf("%w: GitLab has no commit report surface; the MR comment carries the diff coverage table", forge.ErrNotImplemented)
}

var _ forge.Forge = (*Client)(nil)
