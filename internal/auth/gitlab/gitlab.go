// Package gitlab implements auth.Provider for GitLab (gitlab.com) using
// only the standard library. The OAuth application needs the read-only
// "read_user" and "read_api" scopes, nothing broader.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/auth"
)

// Default endpoints. The OAuth pages live on the website host, the
// identity endpoints on the API host.
const (
	DefaultAuthBaseURL = "https://gitlab.com/oauth"
	DefaultAPIBaseURL  = "https://gitlab.com/api/v4"
)

// maxGroupPages bounds the membership listing; nobody authorized to sign
// in is plausibly beyond 10 pages of 100 groups.
const maxGroupPages = 10

// Provider is a GitLab OAuth application. Key (application id) and Secret
// are required; the base URLs default to the public gitlab.com endpoints
// and exist as fields so tests (and one day self-managed instances) can
// point them elsewhere.
type Provider struct {
	Key         string
	Secret      string
	AuthBaseURL string
	APIBaseURL  string
	HTTPClient  *http.Client
}

// New builds a Provider for the public gitlab.com.
func New(key, secret string) *Provider {
	return &Provider{
		Key:        key,
		Secret:     secret,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Provider) Name() string { return "gitlab" }

func (p *Provider) authBase() string {
	if p.AuthBaseURL != "" {
		return p.AuthBaseURL
	}
	return DefaultAuthBaseURL
}

func (p *Provider) apiBase() string {
	if p.APIBaseURL != "" {
		return p.APIBaseURL
	}
	return DefaultAPIBaseURL
}

func (p *Provider) client() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (p *Provider) AuthorizeURL(state, redirectURI string) string {
	q := url.Values{
		"client_id":     {p.Key},
		"state":         {state},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		// read_user covers the identity; read_api exposes the account's
		// group memberships for the login-time authorization check.
		"scope": {"read_user read_api"},
	}
	return p.authBase() + "/authorize?" + q.Encode()
}

func (p *Provider) Identity(ctx context.Context, code, redirectURI string) (*auth.Identity, error) {
	token, err := p.exchange(ctx, code, redirectURI)
	if err != nil {
		return nil, err
	}
	// The token lives only in this frame; it is never persisted.
	var user struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
		Email    string `json:"email"`
	}
	if err := p.get(ctx, token, "/user", &user); err != nil {
		return nil, err
	}
	if user.ID == 0 {
		return nil, fmt.Errorf("gitlab: /user returned no account id")
	}
	// The numeric account id is the stable identifier; usernames can be
	// renamed and released to others (symmetric with the GitHub decision).
	id := &auth.Identity{
		ForgeUUID:   strconv.FormatInt(user.ID, 10),
		DisplayName: user.Name,
		Email:       user.Email,
	}
	if id.DisplayName == "" {
		id.DisplayName = user.Username
	}
	groups, err := p.groups(ctx, token)
	if err != nil {
		return nil, err
	}
	// The username counts as a workspace of its own, so projects under
	// the user's personal namespace admit their owner (same rule as
	// GitHub).
	id.Workspaces = append(groups, user.Username)
	return id, nil
}

// exchange trades the authorization code for an access token.
func (p *Provider) exchange(ctx context.Context, code, redirectURI string) (string, error) {
	form := url.Values{
		"client_id":     {p.Key},
		"client_secret": {p.Secret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.authBase()+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("gitlab: token exchange: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("gitlab: token exchange: status %d: %s", resp.StatusCode, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok); err != nil {
		return "", fmt.Errorf("gitlab: token exchange: %w", err)
	}
	if tok.Error != "" {
		return "", fmt.Errorf("gitlab: token exchange: %s", tok.Error)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("gitlab: token exchange returned no access token")
	}
	return tok.AccessToken, nil
}

// groups lists the full paths of the groups the account is a member of
// (GET /groups?min_access_level=10), subgroups included — a workspace can
// be registered at any level of the namespace tree (D2), so every path
// the user belongs to is a candidate. Follows Link-header pagination.
func (p *Provider) groups(ctx context.Context, token string) ([]string, error) {
	var out []string
	next := p.apiBase() + "/groups?min_access_level=10&per_page=100"
	for range maxGroupPages {
		var page []struct {
			FullPath string `json:"full_path"`
		}
		link, err := p.getURL(ctx, token, next, &page)
		if err != nil {
			return nil, err
		}
		for _, g := range page {
			if g.FullPath != "" {
				out = append(out, g.FullPath)
			}
		}
		next = nextLink(link)
		if next == "" {
			return out, nil
		}
	}
	return out, nil
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

func (p *Provider) get(ctx context.Context, token, path string, dst any) error {
	_, err := p.getURL(ctx, token, p.apiBase()+path, dst)
	return err
}

// getURL fetches u into dst and returns the response's Link header for
// pagination.
func (p *Provider) getURL(ctx context.Context, token, u string, dst any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("gitlab: GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("gitlab: GET %s: status %d: %s", u, resp.StatusCode, body)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(dst); err != nil {
		return "", fmt.Errorf("gitlab: GET %s: %w", u, err)
	}
	return resp.Header.Get("Link"), nil
}

// ensure interface compliance
var _ auth.Provider = (*Provider)(nil)
