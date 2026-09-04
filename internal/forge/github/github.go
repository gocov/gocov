// Package github implements forge.Forge for GitHub (cloud github.com).
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/rest"
)

// DefaultBaseURL is the GitHub REST API root. Kept a field on Client so
// tests (and one day GitHub Enterprise Server) can point elsewhere.
const DefaultBaseURL = "https://api.github.com"

// Client implements forge.Forge against the GitHub REST API using a
// personal access token (classic or fine-grained) for authentication.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// authorize sets the request's auth header. The single seam a future
// GitHub App integration (installation tokens with expiry) replaces.
func (c *Client) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

// api is the request plumbing, bound to this client's credentials.
func (c *Client) api() *rest.Client {
	return &rest.Client{Name: "github", BaseURL: c.BaseURL, HTTPClient: c.HTTPClient, Authorize: c.authorize}
}

var stateNames = map[string]string{
	forge.StateSuccessful: "success",
	forge.StateFailed:     "failure",
	forge.StateInProgress: "pending",
}

// statusMaxDescription is GitHub's cap on commit status descriptions.
const statusMaxDescription = 140

// PostBuildStatus writes a commit status via POST /repos/{slug}/statuses/{sha}.
// The status context is the short Name ("gocov") — that is the string
// branch protection rules match on — with Key as the fallback.
func (c *Client) PostBuildStatus(ctx context.Context, repoSlug, commitSHA string, status forge.BuildStatus) error {
	state, ok := stateNames[status.State]
	if !ok {
		return fmt.Errorf("github: unknown build status state %q", status.State)
	}
	statusContext := status.Name
	if statusContext == "" {
		statusContext = status.Key
	}
	desc := status.Description
	if r := []rune(desc); len(r) > statusMaxDescription {
		desc = string(r[:statusMaxDescription-1]) + "…"
	}
	body := map[string]string{
		"state":       state,
		"context":     statusContext,
		"description": desc,
	}
	if status.URL != "" {
		body["target_url"] = status.URL
	}
	path := fmt.Sprintf("/repos/%s/statuses/%s", repoSlug, url.PathEscape(commitSHA))
	return c.api().Send(ctx, http.MethodPost, path, body)
}

// PostPRComment adds a comment via POST /repos/{slug}/issues/{n}/comments
// (PR conversation comments are issue comments in the GitHub API).
func (c *Client) PostPRComment(ctx context.Context, repoSlug, prID, body string) error {
	path := fmt.Sprintf("/repos/%s/issues/%s/comments", repoSlug, url.PathEscape(prID))
	return c.api().Send(ctx, http.MethodPost, path, map[string]string{"body": body})
}

// maxCommentPages bounds pagination when searching for an earlier comment.
const maxCommentPages = 10

// FindPRComment returns the id of the newest PR conversation comment whose
// body starts with prefix. Matching is marker-based without an author
// check: resolving the credential identity would require an extra token
// scope, and a foreign look-alike comment cannot durably capture the slot
// because GitHub rejects edits of comments the token does not own, which
// makes the caller fall back to posting a fresh comment. GitHub lists
// issue comments oldest first with no sort parameter, so all pages are
// walked and the last match wins.
func (c *Client) FindPRComment(ctx context.Context, repoSlug, prID, prefix string) (string, error) {
	next := fmt.Sprintf("%s/repos/%s/issues/%s/comments?per_page=100",
		c.BaseURL, repoSlug, url.PathEscape(prID))
	found := ""
	for page := 0; next != "" && page < maxCommentPages; page++ {
		var comments []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
		}
		var err error
		if next, err = c.api().GetPage(ctx, next, &comments); err != nil {
			return "", err
		}
		for _, cm := range comments {
			if strings.HasPrefix(cm.Body, prefix) {
				found = strconv.FormatInt(cm.ID, 10)
			}
		}
	}
	return found, nil
}

// UpdatePRComment replaces a comment's body via
// PATCH /repos/{slug}/issues/comments/{comment_id}. Issue comment ids are
// repo-scoped, so the PR id plays no part in the URL.
func (c *Client) UpdatePRComment(ctx context.Context, repoSlug, prID, commentID, body string) error {
	path := fmt.Sprintf("/repos/%s/issues/comments/%s", repoSlug, url.PathEscape(commentID))
	return c.api().Send(ctx, http.MethodPatch, path, map[string]string{"body": body})
}

// maxDiffBytes bounds PR diffs; larger diffs error instead of truncating.
const maxDiffBytes = 32 << 20

// GetPRDiff fetches the unified diff of a pull request via
// GET /repos/{slug}/pulls/{n} with the diff media type.
func (c *Client) GetPRDiff(ctx context.Context, repoSlug, prID string) (string, error) {
	path := fmt.Sprintf("/repos/%s/pulls/%s", repoSlug, url.PathEscape(prID))
	body, err := c.api().GetBytes(ctx, path, "application/vnd.github.v3.diff", maxDiffBytes)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// fetchRepo GETs the repository resource (GET /repos/{slug}) and decodes
// it into out — the request/status/decode plumbing GetDefaultBranch and
// GetRepoVisibility share.
func (c *Client) fetchRepo(ctx context.Context, repoSlug string, out any) error {
	err := c.api().Get(ctx, "/repos/"+repoSlug, out)
	if rest.Status(err) == http.StatusNotFound {
		return fmt.Errorf("%w: %s", forge.ErrRepoNotFound, repoSlug)
	}
	return err
}

// GetDefaultBranch reads the repo's default branch via GET /repos/{slug}.
func (c *Client) GetDefaultBranch(ctx context.Context, repoSlug string) (string, error) {
	var body struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.fetchRepo(ctx, repoSlug, &body); err != nil {
		return "", err
	}
	if body.DefaultBranch == "" {
		return "", fmt.Errorf("github: repository %s has no default branch", repoSlug)
	}
	return body.DefaultBranch, nil
}

// GetRepoVisibility reads the repo's private flag via GET /repos/{slug}.
func (c *Client) GetRepoVisibility(ctx context.Context, repoSlug string) (string, error) {
	var body struct {
		Private bool `json:"private"`
	}
	if err := c.fetchRepo(ctx, repoSlug, &body); err != nil {
		return "", err
	}
	if body.Private {
		return forge.VisibilityPrivate, nil
	}
	return forge.VisibilityPublic, nil
}

// GetRepoID is not needed on GitHub: its Actions OIDC tokens name the repo
// by slug (the "repository" claim), so there is no opaque id to resolve.
func (c *Client) GetRepoID(ctx context.Context, repoSlug string) (string, error) {
	return "", forge.ErrNotImplemented
}

// maxFileBytes bounds source files fetched for the source view.
const maxFileBytes = 2 << 20

// GetFileContent reads a file at a commit via
// GET /repos/{slug}/contents/{path}?ref={sha} with the raw media type.
func (c *Client) GetFileContent(ctx context.Context, repoSlug, commitSHA, path string) ([]byte, error) {
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	reqPath := fmt.Sprintf("/repos/%s/contents/%s?ref=%s",
		repoSlug, strings.Join(segments, "/"), url.QueryEscape(commitSHA))
	data, err := c.api().GetBytes(ctx, reqPath, "application/vnd.github.raw+json", maxFileBytes)
	if rest.Status(err) == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s at %s", forge.ErrRepoNotFound, path, commitSHA)
	}
	return data, err
}

var reportConclusions = map[string]string{
	forge.ReportPassed: "success",
	forge.ReportFailed: "failure",
	"":                 "neutral", // data without a verdict: no gate configured
}

// annotationsPerRequest is the Checks API's cap per request; further
// annotations are appended through the update endpoint.
const annotationsPerRequest = 50

// PublishReport attaches the coverage report to the commit as a check
// run named after the report title, with one inline annotation per
// uncovered range. Every publish creates a fresh run: GitHub surfaces
// only the newest run per name and commit, which yields the replace
// semantics the contract demands — updating the previous run instead
// would append its annotations to the stale ones, never remove them.
func (c *Client) PublishReport(ctx context.Context, repoSlug, commitSHA string, report forge.Report, annotations []forge.Annotation) error {
	conclusion, ok := reportConclusions[report.Result]
	if !ok {
		return fmt.Errorf("github: unknown report result %q", report.Result)
	}
	output := map[string]any{
		"title":   report.Details,
		"summary": checkRunSummary(report),
	}
	first := annotations
	if len(first) > annotationsPerRequest {
		first = first[:annotationsPerRequest]
	}
	if len(first) > 0 {
		output["annotations"] = checkRunAnnotations(first)
	}
	payload := map[string]any{
		"name":        report.Title,
		"head_sha":    commitSHA,
		"external_id": "gocov",
		"status":      "completed",
		"conclusion":  conclusion,
		"output":      output,
	}
	if report.Link != "" {
		payload["details_url"] = report.Link
	}
	var created struct {
		ID int64 `json:"id"`
	}
	path := fmt.Sprintf("/repos/%s/check-runs", repoSlug)
	if err := c.api().JSON(ctx, http.MethodPost, path, payload, &created); err != nil {
		return checkRunError(err)
	}

	// Annotations beyond the per-request cap are appended through the
	// update endpoint, batch by batch, onto the run just created.
	for start := annotationsPerRequest; start < len(annotations); start += annotationsPerRequest {
		end := min(start+annotationsPerRequest, len(annotations))
		batch := map[string]any{
			"output": map[string]any{
				"title":       report.Details,
				"summary":     checkRunSummary(report),
				"annotations": checkRunAnnotations(annotations[start:end]),
			},
		}
		updatePath := fmt.Sprintf("%s/%d", path, created.ID)
		if err := c.api().Send(ctx, http.MethodPatch, updatePath, batch); err != nil {
			return checkRunError(err)
		}
	}
	return nil
}

// checkRunError maps the Checks API's 403 to a wrapped ErrNotImplemented:
// GitHub reserves check-run writes for GitHub Apps (and, with a Checks
// write grant, fine-grained tokens) — for a plain token that is a
// platform limit, not a configuration accident worth failing over. The
// commit status and PR comment still carry the coverage verdict.
func checkRunError(err error) error {
	if rest.Status(err) == http.StatusForbidden {
		return fmt.Errorf("%w: this GitHub credential cannot write check runs (needs a fine-grained token with checks:write, or a GitHub App): %v", forge.ErrNotImplemented, err)
	}
	return err
}

// checkRunSummary renders the report card as the check run's markdown
// summary: the data fields as a table, details on top.
func checkRunSummary(report forge.Report) string {
	var sb strings.Builder
	sb.WriteString(report.Details)
	if len(report.Data) > 0 {
		sb.WriteString("\n\n|   |   |\n| --- | --- |\n")
		for _, d := range report.Data {
			sb.WriteString("| ")
			sb.WriteString(strings.ReplaceAll(d.Title, "|", "\\|"))
			sb.WriteString(" | ")
			sb.WriteString(reportValue(d))
			sb.WriteString(" |\n")
		}
	}
	return sb.String()
}

// reportValue formats one report data value for the summary table.
func reportValue(d forge.ReportData) string {
	switch v := d.Value.(type) {
	case float64:
		if d.Type == forge.DataPercentage {
			return fmt.Sprintf("%.1f%%", v)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		return strings.ReplaceAll(v, "|", "\\|")
	default:
		return fmt.Sprint(v)
	}
}

// checkRunAnnotations maps forge annotations to Checks API objects. The
// API has no file-level annotations, so Line 0 anchors at line 1; an
// untested line is a warning, not a failure.
func checkRunAnnotations(anns []forge.Annotation) []map[string]any {
	items := make([]map[string]any, 0, len(anns))
	for _, a := range anns {
		start, end := a.Line, a.EndLine
		if start == 0 {
			start = 1
		}
		if end < start {
			end = start
		}
		items = append(items, map[string]any{
			"path":             a.Path,
			"start_line":       start,
			"end_line":         end,
			"annotation_level": "warning",
			"message":          a.Summary,
		})
	}
	return items
}

var _ forge.Forge = (*Client)(nil)
