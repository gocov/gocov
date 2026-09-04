// Package rest is the request plumbing the forge clients and the sign-in
// providers share: one place that builds a call, authorizes it, bounds
// what it reads back and turns a refused answer into an error every forge
// shapes the same way. What a path means, what to send and which status
// means what stay in the forge and auth packages — this knows REST, not
// GitHub.
package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

// Client sends calls to one REST API.
type Client struct {
	// Name prefixes every error: "github", "bitbucket", "github app".
	Name string
	// BaseURL is prepended to a call made by path (a URL starting with
	// "/"); an absolute URL — a pagination link — is used as is.
	BaseURL string
	// HTTPClient does the sending; http.DefaultClient when nil.
	HTTPClient *http.Client
	// Authorize sets a request's credentials; nil sends it bare.
	Authorize func(*http.Request)
}

// NewHTTPClient is the sender a caller gets when it brings none of its
// own: bounded by a timeout, because a forge that stops answering must
// not hold an upload — or a sign-in — open indefinitely.
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// Bearer authorizes requests with an OAuth-style bearer token.
func Bearer(token string) func(*http.Request) {
	return func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// Basic authorizes requests with HTTP Basic credentials — how an OAuth
// token endpoint takes the client id and secret when it does not want
// them in the form.
func Basic(user, password string) func(*http.Request) {
	return func(req *http.Request) {
		req.SetBasicAuth(user, password)
	}
}

// Error is a call the API refused: any answer outside 2xx. The status
// and the answer's own explanation are kept so callers can recognize
// the rejections they know how to handle.
type Error struct {
	Status int
	// Body is the start of the answer (at most 4 KiB), usually the API's
	// own explanation.
	Body string
	msg  string
}

func (e *Error) Error() string { return e.msg }

// Status returns the HTTP status of the refusal behind err, or 0 when
// err is anything else (a transport failure, a decode error, nil).
func Status(err error) int {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Status
	}
	return 0
}

// Body returns the explanation the API sent with the refusal behind
// err, or "" when err is anything else. With Status it lets a caller
// recognize a specific rejection without unpacking the error itself.
func Body(err error) string {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Body
	}
	return ""
}

// OAuthErrorCode returns the "error" code a token endpoint put in the
// refusal behind err — "invalid_grant" and the like — or "" when err is
// anything else or the body is not such an answer.
func OAuthErrorCode(err error) string {
	e, ok := errors.AsType[*Error](err)
	if !ok {
		return ""
	}
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(e.Body), &body)
	return body.Error
}

// Token is what a token endpoint answers a successful grant with, in
// the shape RFC 6749 gives it — the fields every forge's OAuth server
// sends the same way. Decode a PostForm answer into it.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	// ExpiresIn is the access token's lifetime in seconds; 0 when the
	// endpoint did not say.
	ExpiresIn float64 `json:"expires_in"`
}

// TTL is the access token's lifetime as a duration — 0 when the
// endpoint did not send one, so callers can fall back to what they know
// about the forge.
func (t Token) TTL() time.Duration {
	return time.Duration(t.ExpiresIn) * time.Second
}

const (
	// maxErrorBody bounds what a refusal's message carries.
	maxErrorBody = 4096
	// maxJSONBody bounds a decoded answer; a listing page is the
	// largest thing decoded through here.
	maxJSONBody = 8 << 20
)

// Get decodes the JSON resource at url into out.
func (c *Client) Get(ctx context.Context, url string, out any) error {
	return c.JSON(ctx, http.MethodGet, url, nil, out)
}

// Send sends payload as JSON — no body at all when nil — and discards
// the answer.
func (c *Client) Send(ctx context.Context, method, url string, payload any) error {
	return c.JSON(ctx, method, url, payload, nil)
}

// JSON sends payload as JSON — no body at all when nil — and, when out
// is non-nil, decodes the answer into it.
func (c *Client) JSON(ctx context.Context, method, url string, payload, out any) error {
	resp, err := c.Do(ctx, method, url, payload, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.decode(resp, url, out)
}

// PostForm sends form-encoded fields and decodes the JSON answer into
// out — the shape of an OAuth token endpoint. The Accept header names
// JSON because GitHub otherwise answers in its legacy query-string form.
// A refused answer is an *Error whose Body carries the endpoint's own
// error code, which is how a dead grant is told from a transient fault.
func (c *Client) PostForm(ctx context.Context, url string, form neturl.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.target(url), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.send(req, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.decode(resp, url, out)
}

// GetPage decodes one page of a listing into out and returns the next
// page's URL from the Link header — "" on the last page.
func (c *Client) GetPage(ctx context.Context, url string, out any) (next string, err error) {
	resp, err := c.Do(ctx, http.MethodGet, url, nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := c.decode(resp, url, out); err != nil {
		return "", err
	}
	return NextLink(resp.Header.Get("Link")), nil
}

// GetBytes reads the raw resource at url, asking for the media type
// accept when set. An answer beyond max bytes is an error, not a
// truncation: a cut-off diff or source file would silently mean wrong
// numbers.
func (c *Client) GetBytes(ctx context.Context, url, accept string, max int64) ([]byte, error) {
	resp, err := c.Do(ctx, http.MethodGet, url, nil, accept)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("%s: reading %s: %w", c.Name, url, err)
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s: %s is larger than %d MiB", c.Name, url, max>>20)
	}
	return data, nil
}

// Do sends one call and returns its 2xx answer with the body still open
// — the caller closes it. Any other status comes back as an *Error with
// the body already consumed; a transport failure is wrapped under Name.
// The higher-level methods cover the common shapes; this is for answers
// that need their own reading.
func (c *Client) Do(ctx context.Context, method, url string, payload any, accept string) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.target(url), body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	return c.send(req, url)
}

// target resolves a call's URL: a path is joined to BaseURL, an absolute
// URL — a pagination link, a token endpoint — is used as is.
func (c *Client) target(url string) string {
	if strings.HasPrefix(url, "/") {
		return c.BaseURL + url
	}
	return url
}

// send authorizes a built request, sends it and applies the answer
// rule every call shares: 2xx comes back open, anything else as *Error.
func (c *Client) send(req *http.Request, url string) (*http.Response, error) {
	if c.Authorize != nil {
		c.Authorize(req)
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.Name, err)
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		resp.Body.Close()
		return nil, &Error{
			Status: resp.StatusCode,
			Body:   string(msg),
			msg:    fmt.Sprintf("%s: %s returned %d: %s", c.Name, url, resp.StatusCode, msg),
		}
	}
	return resp, nil
}

func (c *Client) decode(resp *http.Response, url string, out any) error {
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONBody)).Decode(out); err != nil {
		return fmt.Errorf("%s: decoding %s: %w", c.Name, url, err)
	}
	return nil
}

// EscapePath escapes a slash-separated path one segment at a time, so
// the slashes keep structuring the URL while everything between them
// travels safely — a file path inside a repository URL, where a bare
// "?" or "#" in a filename would otherwise cut the request short.
func EscapePath(path string) string {
	var b strings.Builder
	for i, segment := range strings.Split(path, "/") {
		if i > 0 {
			b.WriteByte('/')
		}
		b.WriteString(neturl.PathEscape(segment))
	}
	return b.String()
}

// NextLink extracts the rel="next" URL from a Link response header, or
// "" when there is no next page.
func NextLink(header string) string {
	for part := range strings.SplitSeq(header, ",") {
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
