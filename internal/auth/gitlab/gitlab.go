// Package gitlab implements auth.Provider for GitLab (gitlab.com) using
// only the standard library. The OAuth application needs the read-only
// "read_user" and "read_api" scopes, nothing broader.
package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gocov/gocov/internal/auth"
	"github.com/gocov/gocov/internal/rest"
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
	if err := p.api(token).Get(ctx, "/user", &user); err != nil {
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
	groups, err := p.groups(ctx, token, accessLevelMember)
	if err != nil {
		return nil, err
	}
	owned, err := p.groups(ctx, token, accessLevelOwner)
	if err != nil {
		return nil, err
	}
	// The username counts as a workspace of its own, so projects under
	// the user's personal namespace admit their owner (same rule as
	// GitHub) — and that account owns it.
	id.Workspaces = append(groups, user.Username)
	id.OwnedWorkspaces = append(owned, user.Username)
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
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	c := &rest.Client{Name: "gitlab", HTTPClient: p.client()}
	if err := c.PostForm(ctx, p.authBase()+"/token", form, &tok); err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	if tok.Error != "" {
		return "", fmt.Errorf("gitlab: token exchange: %s", tok.Error)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("gitlab: token exchange returned no access token")
	}
	return tok.AccessToken, nil
}

// GitLab access levels, as the groups listing's min_access_level filter
// takes them. Guest (10) is the lowest level that is still membership;
// Owner (50) is the level that administers the group. Maintainer (40)
// deliberately does not count as an owner here.
const (
	accessLevelMember = 10
	accessLevelOwner  = 50
)

// groups lists the full paths of the groups the account belongs to at
// minAccess or above (GET /groups?min_access_level=N), subgroups included
// — a workspace can be registered at any level of the namespace tree
// (D2), so every path the user belongs to is a candidate. Follows
// Link-header pagination.
func (p *Provider) groups(ctx context.Context, token string, minAccess int) ([]string, error) {
	var out []string
	api := p.api(token)
	next := "/groups?min_access_level=" + strconv.Itoa(minAccess) + "&per_page=100"
	for range maxGroupPages {
		var page []struct {
			FullPath string `json:"full_path"`
		}
		link, err := api.GetPage(ctx, next, &page)
		if err != nil {
			return nil, err
		}
		for _, g := range page {
			if g.FullPath != "" {
				out = append(out, g.FullPath)
			}
		}
		if next = link; next == "" {
			return out, nil
		}
	}
	return out, nil
}

// api is the request plumbing for the identity endpoints, bound to the
// exchanged token.
func (p *Provider) api(token string) *rest.Client {
	return &rest.Client{Name: "gitlab", BaseURL: p.apiBase(), HTTPClient: p.client(), Authorize: rest.Bearer(token)}
}

// ensure interface compliance
var _ auth.Provider = (*Provider)(nil)
