package bitbucket

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/rest"
)

// Workspace-connect OAuth grant (One-Click Connect P2/D6). Consumer is
// the deployment's Bitbucket OAuth consumer — the same one that powers
// sign-in — used here for the bigger "Connect workspace" grant whose
// refresh token is stored on the workspace.
//
// Rotation, enforced by Bitbucket since 2026-05-04: every refresh_token
// grant returns a NEW refresh token and invalidates the old one, with no
// documented grace window, and unused refresh tokens expire after three
// months (full re-consent required). Callers must persist the rotated
// token immediately; the server side does the atomic store-and-swap.

// DefaultAuthBaseURL is the OAuth endpoint root on the website host.
const DefaultAuthBaseURL = "https://bitbucket.org/site/oauth2"

// Consumer is a Bitbucket OAuth consumer used for workspace grants. Key
// and Secret are required; the base URLs default to the public hosts and
// exist as fields so tests can point them at a local server.
type Consumer struct {
	Key         string
	Secret      string
	AuthBaseURL string
	APIBaseURL  string
	HTTPClient  *http.Client
}

// Grant is one issued (or refreshed) token set. Account is the granting
// Bitbucket account — comments will visibly post as it (D8) — and TTL
// is two hours on live Bitbucket.
type Grant = forge.Grant

func (c *Consumer) authBase() string { return cmp.Or(c.AuthBaseURL, DefaultAuthBaseURL) }

func (c *Consumer) apiBase() string { return cmp.Or(c.APIBaseURL, DefaultBaseURL) }

func (c *Consumer) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return rest.NewHTTPClient()
}

// AuthorizeURL is the consent page for the connect grant. No scope
// parameter: Bitbucket scopes are configured on the consumer, and the
// connect grant wants everything the consumer carries.
func (c *Consumer) AuthorizeURL(state, redirectURI string) string {
	q := url.Values{
		"client_id":     {c.Key},
		"response_type": {"code"},
		"state":         {state},
		"redirect_uri":  {redirectURI},
	}
	return c.authBase() + "/authorize?" + q.Encode()
}

// Exchange trades the authorization code for the grant and resolves the
// granting account's username (D8: the identity comments will carry).
func (c *Consumer) Exchange(ctx context.Context, code, redirectURI string) (*Grant, error) {
	grant, err := c.token(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	})
	if err != nil {
		return nil, err
	}
	account, err := c.username(ctx, grant.AccessToken)
	if err != nil {
		return nil, err
	}
	grant.Account = account
	return grant, nil
}

// Refresh trades a refresh token for a fresh access token — and, since
// Bitbucket rotates, a NEW refresh token that must replace the stored
// one. An invalid_grant answer means the grant is gone (revoked, the
// account left, or the token aged out unused) and maps to
// ErrCredentialsRevoked.
func (c *Consumer) Refresh(ctx context.Context, refreshToken string) (*Grant, error) {
	return c.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

// ForgeClient returns a forge client acting through the grant's access
// token (Bearer — Bitbucket dropped query-param and body tokens with
// CHANGE-3052).
func (c *Consumer) ForgeClient(accessToken string) forge.Forge {
	return &Client{
		BaseURL:     c.apiBase(),
		AccessToken: accessToken,
		HTTPClient:  c.client(),
	}
}

// token runs one grant against the token endpoint with HTTP Basic
// consumer auth.
func (c *Consumer) token(ctx context.Context, form url.Values) (*Grant, error) {
	var tok rest.Token
	api := &rest.Client{Name: "bitbucket", BaseURL: c.authBase(), HTTPClient: c.client(), Authorize: rest.Basic(c.Key, c.Secret)}
	err := api.PostForm(ctx, "/access_token", form, &tok)
	if code := rest.OAuthErrorCode(err); code != "" {
		// Dead-grant answers the lazy-detection path keys off. RFC 6749
		// says invalid_grant, but live Bitbucket (probed 2026-08-09)
		// answers a rotated-away refresh token with unauthorized_client
		// ("Invalid OAuth client credentials") — and briefly answers the
		// same for VALID tokens right after such a failure (a short
		// client lockout; the valid token worked again minutes later,
		// so there is no grant-family revocation). Treating refresh-time
		// unauthorized_client as revocation is still the right call: a
		// lockout-flagged workspace degrades identically either way and
		// the broken flag self-heals on the next successful refresh,
		// while the alternative would leave truly dead grants without a
		// reconnect prompt forever. On a code exchange the same answer
		// far more likely means misconfigured consumer credentials,
		// which must not read as a revoked workspace.
		refreshing := form.Get("grant_type") == "refresh_token"
		if code == "invalid_grant" || (refreshing && code == "unauthorized_client") {
			return nil, fmt.Errorf("%w: %w", forge.ErrCredentialsRevoked, err)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("token grant: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("bitbucket: token grant returned no access token")
	}
	return &Grant{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, TTL: tok.TTL()}, nil
}

// username resolves the token's account via GET /user.
func (c *Consumer) username(ctx context.Context, accessToken string) (string, error) {
	api := &rest.Client{Name: "bitbucket", BaseURL: c.apiBase(), HTTPClient: c.client(), Authorize: rest.Bearer(accessToken)}
	var user struct {
		Username string `json:"username"`
	}
	if err := api.Get(ctx, "/user", &user); err != nil {
		return "", err
	}
	if user.Username == "" {
		return "", fmt.Errorf("bitbucket: GET /user returned no username")
	}
	return user.Username, nil
}
