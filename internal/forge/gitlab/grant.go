package gitlab

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/rest"
)

// Workspace-connect OAuth grant (GitLab Connect, the P2 one-click item).
// Application is the deployment's GitLab OAuth application — the same
// one that powers sign-in — used here for the bigger "Connect workspace"
// grant whose refresh token is stored on the workspace. GitLab lets an
// authorization request ask for a subset of the application's registered
// scopes, so sign-in keeps its read-only consent while connect asks for
// "api"; the application itself must carry the api scope for that.
//
// GitLab rotates refresh tokens: every refresh_token grant returns a NEW
// refresh token and invalidates the old one. Callers must persist the
// rotated token immediately; the server side does the atomic
// store-and-swap.

// DefaultAuthBaseURL is the OAuth endpoint root on the website host.
const DefaultAuthBaseURL = "https://gitlab.com/oauth"

// grantTTLDefault covers token answers without expires_in (OAuth
// applications created before GitLab enabled token expiry); GitLab's
// expiring tokens live two hours.
const grantTTLDefault = 2 * time.Hour

// Application is a GitLab OAuth application used for workspace grants.
// Key (application id) and Secret are required; the base URLs default to
// the public gitlab.com endpoints and exist as fields so tests can point
// them at a local server.
type Application struct {
	Key         string
	Secret      string
	AuthBaseURL string
	APIBaseURL  string
	HTTPClient  *http.Client
}

// Grant is one issued (or refreshed) token set. Account is the granting
// GitLab account — notes will visibly post as it (the Bitbucket D8
// caveat applies) — and TTL is two hours on gitlab.com.
type Grant = forge.Grant

func (a *Application) authBase() string { return cmp.Or(a.AuthBaseURL, DefaultAuthBaseURL) }

func (a *Application) apiBase() string { return cmp.Or(a.APIBaseURL, DefaultBaseURL) }

func (a *Application) client() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return rest.NewHTTPClient()
}

// AuthorizeURL is the consent page for the connect grant. Unlike
// sign-in's read-only scopes, connect asks for "api" — statuses, notes
// and diffs all write through it.
func (a *Application) AuthorizeURL(state, redirectURI string) string {
	q := url.Values{
		"client_id":     {a.Key},
		"response_type": {"code"},
		"state":         {state},
		"redirect_uri":  {redirectURI},
		"scope":         {"api"},
	}
	return a.authBase() + "/authorize?" + q.Encode()
}

// Exchange trades the authorization code for the grant and resolves the
// granting account's username (the identity notes will carry).
func (a *Application) Exchange(ctx context.Context, code, redirectURI string) (*Grant, error) {
	grant, err := a.token(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	})
	if err != nil {
		return nil, err
	}
	account, err := a.username(ctx, grant.AccessToken)
	if err != nil {
		return nil, err
	}
	grant.Account = account
	return grant, nil
}

// Refresh trades a refresh token for a fresh access token — and, since
// GitLab rotates, a NEW refresh token that must replace the stored one.
// An invalid_grant answer means the grant is gone (revoked on the
// account's applications page, or the token rotated away) and maps to
// ErrCredentialsRevoked. The redirect URI must accompany the refresh —
// GitLab's token endpoint expects it on every grant.
func (a *Application) Refresh(ctx context.Context, refreshToken, redirectURI string) (*Grant, error) {
	return a.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"redirect_uri":  {redirectURI},
	})
}

// ForgeClient returns a forge client acting through the grant's access
// token (the Client's Bearer header serves OAuth tokens too).
func (a *Application) ForgeClient(accessToken string) forge.Forge {
	return &Client{
		BaseURL:    a.apiBase(),
		Token:      accessToken,
		HTTPClient: a.client(),
	}
}

// token runs one grant against the token endpoint. Client credentials
// ride in the form body, same as the sign-in provider's exchange.
func (a *Application) token(ctx context.Context, form url.Values) (*Grant, error) {
	form.Set("client_id", a.Key)
	form.Set("client_secret", a.Secret)
	var tok rest.Token
	api := &rest.Client{Name: "gitlab", BaseURL: a.authBase(), HTTPClient: a.client()}
	err := api.PostForm(ctx, "/token", form, &tok)
	if rest.OAuthErrorCode(err) == "invalid_grant" {
		return nil, fmt.Errorf("%w: %w", forge.ErrCredentialsRevoked, err)
	}
	if err != nil {
		return nil, fmt.Errorf("token grant: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("gitlab: token grant returned no access token")
	}
	return &Grant{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TTL:          cmp.Or(tok.TTL(), grantTTLDefault),
	}, nil
}

// username resolves the token's account via GET /user.
func (a *Application) username(ctx context.Context, accessToken string) (string, error) {
	api := &rest.Client{Name: "gitlab", BaseURL: a.apiBase(), HTTPClient: a.client(), Authorize: rest.Bearer(accessToken)}
	var user struct {
		Username string `json:"username"`
	}
	if err := api.Get(ctx, "/user", &user); err != nil {
		return "", err
	}
	if user.Username == "" {
		return "", fmt.Errorf("gitlab: GET /user returned no username")
	}
	return user.Username, nil
}
