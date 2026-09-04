// Package bitbucket implements forge.Forge for Bitbucket Cloud.
package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/forge/internal/rest"
)

// DefaultBaseURL is the Bitbucket Cloud REST API root.
const DefaultBaseURL = "https://api.bitbucket.org/2.0"

// Client implements forge.Forge against the Bitbucket Cloud API using an
// app password (or scoped API token) for authentication — or, when
// AccessToken is set, an OAuth access token from the workspace's
// connect grant (One-Click Connect P2).
type Client struct {
	BaseURL     string
	Username    string
	AppPassword string
	// AccessToken switches authentication to OAuth Bearer; Username and
	// AppPassword are then ignored.
	AccessToken string
	HTTPClient  *http.Client
}

// authorize sets the request's auth: the grant's Bearer token when
// connected, HTTP Basic with the stored credential otherwise.
func (c *Client) authorize(req *http.Request) {
	if c.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AccessToken)
		return
	}
	req.SetBasicAuth(c.Username, c.AppPassword)
}

// api is the request plumbing, bound to this client's credentials.
func (c *Client) api() *rest.Client {
	return &rest.Client{Name: "bitbucket", BaseURL: c.BaseURL, HTTPClient: c.HTTPClient, Authorize: c.authorize}
}

var stateNames = map[string]string{
	forge.StateSuccessful: "SUCCESSFUL",
	forge.StateFailed:     "FAILED",
	forge.StateInProgress: "INPROGRESS",
}

// PostBuildStatus writes a build status onto a commit via
// POST /repositories/{slug}/commit/{sha}/statuses/build.
func (c *Client) PostBuildStatus(ctx context.Context, repoSlug, commitSHA string, status forge.BuildStatus) error {
	state, ok := stateNames[status.State]
	if !ok {
		return fmt.Errorf("bitbucket: unknown build status state %q", status.State)
	}
	body := map[string]string{
		"key":         status.Key,
		"state":       state,
		"name":        status.Name,
		"description": status.Description,
		"url":         status.URL,
	}
	path := fmt.Sprintf("/repositories/%s/commit/%s/statuses/build",
		repoSlug, url.PathEscape(commitSHA))
	return c.api().Send(ctx, http.MethodPost, path, body)
}

// PostPRComment adds a comment via
// POST /repositories/{slug}/pullrequests/{id}/comments.
func (c *Client) PostPRComment(ctx context.Context, repoSlug, prID, body string) error {
	payload := map[string]any{
		"content": map[string]string{"raw": body},
	}
	path := fmt.Sprintf("/repositories/%s/pullrequests/%s/comments",
		repoSlug, url.PathEscape(prID))
	return c.api().Send(ctx, http.MethodPost, path, payload)
}

// maxCommentPages bounds pagination when searching for an earlier comment.
const maxCommentPages = 10

// bitbucketUser identifies an account; either field may be empty depending
// on the credential type.
type bitbucketUser struct {
	AccountID string `json:"account_id"`
	UUID      string `json:"uuid"`
}

func (u bitbucketUser) is(other bitbucketUser) bool {
	if u.AccountID != "" && u.AccountID == other.AccountID {
		return true
	}
	return u.UUID != "" && u.UUID == other.UUID
}

// currentUser resolves the authenticated account via GET /user.
func (c *Client) currentUser(ctx context.Context) (bitbucketUser, error) {
	var u bitbucketUser
	if err := c.api().Get(ctx, "/user", &u); err != nil {
		return bitbucketUser{}, err
	}
	if u.AccountID == "" && u.UUID == "" {
		return bitbucketUser{}, fmt.Errorf("bitbucket: /user returned no account identity")
	}
	return u, nil
}

// FindPRComment returns the id of the newest top-level PR comment that was
// authored by the credential account and starts with prefix. Comments are
// requested newest first, so the page cap only bounds how far back the
// search goes; replies, inline comments and other authors never match —
// a stranger's "**gocov**" comment must not be able to capture the slot.
func (c *Client) FindPRComment(ctx context.Context, repoSlug, prID, prefix string) (string, error) {
	self, err := c.currentUser(ctx)
	if err != nil {
		return "", err
	}
	next := fmt.Sprintf("%s/repositories/%s/pullrequests/%s/comments?pagelen=100&sort=-created_on",
		c.BaseURL, repoSlug, url.PathEscape(prID))
	for page := 0; next != "" && page < maxCommentPages; page++ {
		var body struct {
			Values []struct {
				ID      int64 `json:"id"`
				Deleted bool  `json:"deleted"`
				Inline  *struct {
					Path string `json:"path"`
				} `json:"inline"`
				Parent *struct {
					ID int64 `json:"id"`
				} `json:"parent"`
				User    bitbucketUser `json:"user"`
				Content struct {
					Raw string `json:"raw"`
				} `json:"content"`
			} `json:"values"`
			Next string `json:"next"`
		}
		if err := c.api().Get(ctx, next, &body); err != nil {
			return "", err
		}
		for _, v := range body.Values {
			if v.Deleted || v.Inline != nil || v.Parent != nil || !self.is(v.User) {
				continue
			}
			if strings.HasPrefix(v.Content.Raw, prefix) {
				return strconv.FormatInt(v.ID, 10), nil
			}
		}
		next = body.Next
	}
	return "", nil
}

// UpdatePRComment replaces a comment's body via
// PUT /repositories/{slug}/pullrequests/{id}/comments/{comment_id}.
func (c *Client) UpdatePRComment(ctx context.Context, repoSlug, prID, commentID, body string) error {
	payload := map[string]any{
		"content": map[string]string{"raw": body},
	}
	path := fmt.Sprintf("/repositories/%s/pullrequests/%s/comments/%s",
		repoSlug, url.PathEscape(prID), url.PathEscape(commentID))
	return c.api().Send(ctx, http.MethodPut, path, payload)
}

// GetPRDiff fetches the unified diff of a pull request via
// GET /repositories/{slug}/pullrequests/{id}/diff. Bitbucket answers with a
// redirect to the diff blob, which the HTTP client follows transparently.
func (c *Client) GetPRDiff(ctx context.Context, repoSlug, prID string) (string, error) {
	path := fmt.Sprintf("/repositories/%s/pullrequests/%s/diff",
		repoSlug, url.PathEscape(prID))
	body, err := c.api().GetBytes(ctx, path, "", maxDiffBytes)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// maxDiffBytes bounds PR diffs; larger diffs error instead of truncating.
const maxDiffBytes = 32 << 20

// fetchRepository GETs the repository resource (GET /repositories/{slug})
// and decodes it into out — the request/status/decode plumbing
// GetDefaultBranch and GetRepoVisibility share.
func (c *Client) fetchRepository(ctx context.Context, repoSlug string, out any) error {
	err := c.api().Get(ctx, "/repositories/"+repoSlug, out)
	if rest.Status(err) == http.StatusNotFound {
		return fmt.Errorf("%w: %s", forge.ErrRepoNotFound, repoSlug)
	}
	return err
}

// GetDefaultBranch reads the repo's main branch via GET /repositories/{slug}.
func (c *Client) GetDefaultBranch(ctx context.Context, repoSlug string) (string, error) {
	var body struct {
		MainBranch struct {
			Name string `json:"name"`
		} `json:"mainbranch"`
	}
	if err := c.fetchRepository(ctx, repoSlug, &body); err != nil {
		return "", err
	}
	if body.MainBranch.Name == "" {
		return "", fmt.Errorf("bitbucket: repository %s has no main branch", repoSlug)
	}
	return body.MainBranch.Name, nil
}

// GetRepoVisibility reads the repo's is_private flag via
// GET /repositories/{slug}.
func (c *Client) GetRepoVisibility(ctx context.Context, repoSlug string) (string, error) {
	var body struct {
		IsPrivate bool `json:"is_private"`
	}
	if err := c.fetchRepository(ctx, repoSlug, &body); err != nil {
		return "", err
	}
	if body.IsPrivate {
		return forge.VisibilityPrivate, nil
	}
	return forge.VisibilityPublic, nil
}

// GetRepoID reads the repo's UUID via GET /repositories/{slug} — the id a
// Bitbucket Pipelines OIDC token names the repo by (its sub and
// repositoryUuid claims). The braces Bitbucket wraps the UUID in are kept
// as returned; the caller normalizes before comparing.
func (c *Client) GetRepoID(ctx context.Context, repoSlug string) (string, error) {
	var body struct {
		UUID string `json:"uuid"`
	}
	if err := c.fetchRepository(ctx, repoSlug, &body); err != nil {
		return "", err
	}
	if body.UUID == "" {
		return "", fmt.Errorf("bitbucket: repository %s has no uuid", repoSlug)
	}
	return body.UUID, nil
}

// maxFileBytes bounds source files fetched for the source view.
const maxFileBytes = 2 << 20

// GetFileContent reads a file at a commit via
// GET /repositories/{slug}/src/{commit}/{path}.
func (c *Client) GetFileContent(ctx context.Context, repoSlug, commitSHA, path string) ([]byte, error) {
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	reqPath := fmt.Sprintf("/repositories/%s/src/%s/%s",
		repoSlug, url.PathEscape(commitSHA), strings.Join(segments, "/"))
	data, err := c.api().GetBytes(ctx, reqPath, "", maxFileBytes)
	if rest.Status(err) == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s at %s", forge.ErrRepoNotFound, path, commitSHA)
	}
	return data, err
}

// reportID is the stable Code Insights report id: one report per commit,
// so repeated uploads replace instead of stacking.
const reportID = "gocov"

var reportResults = map[string]string{
	forge.ReportPassed: "PASSED",
	forge.ReportFailed: "FAILED",
}

var reportDataTypes = map[string]string{
	forge.DataPercentage: "PERCENTAGE",
	forge.DataNumber:     "NUMBER",
	forge.DataText:       "TEXT",
}

// PublishReport writes a Code Insights report onto a commit via
// PUT /repositories/{slug}/commit/{sha}/reports/gocov, then bulk-uploads
// the annotations. The report is deleted first: annotation bulk upload
// only upserts by external_id, so without the delete a re-upload with
// fewer findings would leave stale annotations in the PR diff.
func (c *Client) PublishReport(ctx context.Context, repoSlug, commitSHA string, report forge.Report, annotations []forge.Annotation) error {
	base := fmt.Sprintf("/repositories/%s/commit/%s/reports/%s",
		repoSlug, url.PathEscape(commitSHA), reportID)
	if err := c.deleteReport(ctx, base); err != nil {
		return err
	}

	payload := map[string]any{
		"title":       report.Title,
		"details":     report.Details,
		"report_type": "COVERAGE",
		"reporter":    "gocov",
	}
	if report.Link != "" {
		payload["link"] = report.Link
	}
	if report.Result != "" {
		result, ok := reportResults[report.Result]
		if !ok {
			return fmt.Errorf("bitbucket: unknown report result %q", report.Result)
		}
		payload["result"] = result
	}
	data := make([]map[string]any, 0, len(report.Data))
	for _, d := range report.Data {
		typeName, ok := reportDataTypes[d.Type]
		if !ok {
			return fmt.Errorf("bitbucket: unknown report data type %q", d.Type)
		}
		data = append(data, map[string]any{
			"title": d.Title,
			"type":  typeName,
			"value": d.Value,
		})
	}
	payload["data"] = data
	if err := c.api().Send(ctx, http.MethodPut, base, payload); err != nil {
		// Bitbucket rejects link values it cannot resolve publicly
		// (e.g. the default http://localhost:8080 base URL) with a 400
		// and drops the whole report. A report without its link beats
		// no report: retry once link-less and keep going.
		if report.Link == "" || !isInvalidLinkError(err) {
			return err
		}
		delete(payload, "link")
		if err := c.api().Send(ctx, http.MethodPut, base, payload); err != nil {
			return err
		}
	}

	if len(annotations) == 0 {
		return nil
	}
	// One bulk request; the API accepts at most 100 annotations per POST
	// and the server never sends more.
	items := make([]map[string]any, 0, len(annotations))
	for i, a := range annotations {
		// An untested line is a smell, not an incident: CODE_SMELL at
		// MEDIUM, never the vulnerability/critical tier.
		item := map[string]any{
			"external_id":     fmt.Sprintf("gocov-%03d", i+1),
			"annotation_type": "CODE_SMELL",
			"severity":        "MEDIUM",
			"path":            a.Path,
			"summary":         a.Summary,
		}
		if a.Line > 0 {
			item["line"] = a.Line
		}
		items = append(items, item)
	}
	return c.api().Send(ctx, http.MethodPost, base+"/annotations", items)
}

// deleteReport removes the commit's existing gocov report and, with it,
// its annotations. A 404 just means there is nothing to delete yet.
func (c *Client) deleteReport(ctx context.Context, path string) error {
	err := c.api().Send(ctx, http.MethodDelete, path, nil)
	if rest.Status(err) == http.StatusNotFound {
		return nil
	}
	return err
}

// isInvalidLinkError recognizes Bitbucket's rejection of a report link,
// e.g. `{"error": {"message": "link is not a valid URL"}}` with a 400.
func isInvalidLinkError(err error) bool {
	e, ok := errors.AsType[*rest.Error](err)
	return ok && e.Status == http.StatusBadRequest &&
		strings.Contains(e.Body, "link is not a valid URL")
}

var _ forge.Forge = (*Client)(nil)
