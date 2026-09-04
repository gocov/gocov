// Package bitbucket implements auth.Provider for Bitbucket Cloud using
// only the standard library. The OAuth consumer needs the read-only
// "account" and "email" scopes, nothing broader.
package bitbucket

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gocov/gocov/internal/auth"
	"github.com/gocov/gocov/internal/rest"
)

// Default endpoints. The OAuth pages live on the website host, the
// identity endpoints on the API host.
const (
	DefaultAuthBaseURL = "https://bitbucket.org/site/oauth2"
	DefaultAPIBaseURL  = "https://api.bitbucket.org/2.0"
)

// maxWorkspacePages bounds the membership listing; nobody authorized to
// sign in is plausibly beyond 10 pages of 100 workspaces.
const maxWorkspacePages = 10

// Provider is a Bitbucket OAuth consumer. Key and Secret are required;
// the base URLs default to the public Bitbucket Cloud endpoints and exist
// as fields so tests can point them at a local server.
type Provider struct {
	Key         string
	Secret      string
	AuthBaseURL string
	APIBaseURL  string
	HTTPClient  *http.Client
}

// New builds a Provider for the public Bitbucket Cloud.
func New(key, secret string) *Provider {
	return &Provider{
		Key:        key,
		Secret:     secret,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Provider) Name() string { return "bitbucket" }

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
		"response_type": {"code"},
		"state":         {state},
		"redirect_uri":  {redirectURI},
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
		UUID        string `json:"uuid"`
		DisplayName string `json:"display_name"`
	}
	if err := p.api(token).Get(ctx, "/user", &user); err != nil {
		return nil, err
	}
	if user.UUID == "" {
		return nil, fmt.Errorf("bitbucket: /user returned no uuid")
	}
	id := &auth.Identity{ForgeUUID: user.UUID, DisplayName: user.DisplayName}
	id.Email, err = p.primaryEmail(ctx, token)
	if err != nil {
		return nil, err
	}
	id.Workspaces, id.OwnedWorkspaces, err = p.workspaces(ctx, token)
	if err != nil {
		return nil, err
	}
	return id, nil
}

// exchange trades the authorization code for an access token.
func (p *Provider) exchange(ctx context.Context, code, redirectURI string) (string, error) {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	c := &rest.Client{Name: "bitbucket", HTTPClient: p.client(), Authorize: rest.Basic(p.Key, p.Secret)}
	if err := c.PostForm(ctx, p.authBase()+"/access_token", form, &tok); err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("bitbucket: token exchange returned no access token")
	}
	return tok.AccessToken, nil
}

// primaryEmail returns the account's primary email address, or "" when
// none is confirmed ("email" scope, GET /user/emails).
func (p *Provider) primaryEmail(ctx context.Context, token string) (string, error) {
	var page struct {
		Values []struct {
			Email     string `json:"email"`
			IsPrimary bool   `json:"is_primary"`
		} `json:"values"`
	}
	if err := p.api(token).Get(ctx, "/user/emails", &page); err != nil {
		return "", err
	}
	for _, v := range page.Values {
		if v.IsPrimary {
			return v.Email, nil
		}
	}
	if len(page.Values) > 0 {
		return page.Values[0].Email, nil
	}
	return "", nil
}

// workspaces lists the slugs of the workspaces the account is a member of
// and, separately, those it administers ("account" scope,
// GET /user/workspaces), following pagination. This is the CHANGE-3022
// replacement API; the older GET /workspaces and
// GET /user/permissions/workspaces listings were sunset by CHANGE-2770
// and answer 410. Each value is a workspace access record: the listing
// flags administrators with an "administrator" boolean, and a
// workspace_membership-shaped record says the same with
// permission "owner" — both are honoured.
func (p *Provider) workspaces(ctx context.Context, token string) (all, admin []string, err error) {
	api := p.api(token)
	next := "/user/workspaces?" + url.Values{"pagelen": {"100"}}.Encode()
	for range maxWorkspacePages {
		var page struct {
			Values []struct {
				Administrator bool   `json:"administrator"`
				Permission    string `json:"permission"`
				Workspace     struct {
					Slug string `json:"slug"`
				} `json:"workspace"`
			} `json:"values"`
			Next string `json:"next"`
		}
		if err := api.Get(ctx, next, &page); err != nil {
			return nil, nil, err
		}
		for _, v := range page.Values {
			if v.Workspace.Slug == "" {
				continue
			}
			all = append(all, v.Workspace.Slug)
			if v.Administrator || v.Permission == "owner" {
				admin = append(admin, v.Workspace.Slug)
			}
		}
		if page.Next == "" {
			return all, admin, nil
		}
		next = page.Next
	}
	return all, admin, nil
}

// api is the request plumbing for the identity endpoints, bound to the
// exchanged token. Bitbucket pages by an absolute "next" URL in the body,
// which the client sends as is.
func (p *Provider) api(token string) *rest.Client {
	return &rest.Client{Name: "bitbucket", BaseURL: p.apiBase(), HTTPClient: p.client(), Authorize: rest.Bearer(token)}
}

// ensure interface compliance
var _ auth.Provider = (*Provider)(nil)
