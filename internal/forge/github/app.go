package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/bykclk/gocov/internal/forge"
)

// App is a GitHub App identity (One-Click Connect D1/D2): the hosted
// instance's official "gocov" app, or a self-hoster's own registration.
// It trades a short-lived RS256 JWT signed with the app key for hour-long
// installation tokens, cached per installation until shortly before
// expiry. Safe for concurrent use.
type App struct {
	AppID      string
	Key        *rsa.PrivateKey
	BaseURL    string // defaults to DefaultBaseURL
	HTTPClient *http.Client

	mu         sync.Mutex
	tokens     map[int64]instToken
	installURL string
}

type instToken struct {
	value     string
	expiresAt time.Time
}

// NewApp parses the PEM private key GitHub issued for the app. GitHub
// downloads PKCS#1 ("RSA PRIVATE KEY"); PKCS#8 is accepted for keys that
// passed through other tooling.
func NewApp(appID string, pemKey []byte) (*App, error) {
	if appID == "" {
		return nil, fmt.Errorf("github app: app id is required")
	}
	block, _ := pem.Decode(pemKey)
	if block == nil {
		return nil, fmt.Errorf("github app: private key is not PEM")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("github app: parsing private key: %w", err)
		}
		key = k
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("github app: parsing private key: %w", err)
		}
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("github app: private key is %T, want RSA", k)
		}
		key = rsaKey
	default:
		return nil, fmt.Errorf("github app: unsupported PEM block %q", block.Type)
	}
	return &App{
		AppID:      appID,
		Key:        key,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		tokens:     map[int64]instToken{},
	}, nil
}

func (a *App) baseURL() string {
	if a.BaseURL != "" {
		return a.BaseURL
	}
	return DefaultBaseURL
}

func (a *App) client() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// appJWT builds the app-authentication JWT: RS256 over the standard iss /
// iat / exp claims, nothing else. iat is backdated a minute against clock
// drift; exp stays under GitHub's 10-minute cap.
func (a *App) appJWT(now time.Time) (string, error) {
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": a.AppID,
	})
	if err != nil {
		return "", err
	}
	signing := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) +
		"." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.Key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("github app: signing jwt: %w", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// tokenLeeway retires cached installation tokens before GitHub does, so a
// token handed out is never moments from expiring mid-upload.
const tokenLeeway = 5 * time.Minute

// installationToken returns a valid token for the installation, minting a
// fresh one when the cache is empty or near expiry; fresh reports which
// of the two happened. A 401/404 from the mint means the installation is
// gone (uninstalled, or the app credentials were rotated away) and maps
// to ErrCredentialsRevoked — lazy uninstall detection (D3) keys off
// exactly this.
func (a *App) installationToken(ctx context.Context, installationID int64) (token string, fresh bool, err error) {
	a.mu.Lock()
	t, ok := a.tokens[installationID]
	a.mu.Unlock()
	if ok && time.Now().Before(t.expiresAt.Add(-tokenLeeway)) {
		return t.value, false, nil
	}

	jwt, err := a.appJWT(time.Now())
	if err != nil {
		return "", false, err
	}
	path := fmt.Sprintf("/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL()+path, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := a.client().Do(req)
	if err != nil {
		return "", false, fmt.Errorf("github app: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", false, fmt.Errorf("%w: github app installation %d: %d: %s",
			forge.ErrCredentialsRevoked, installationID, resp.StatusCode, msg)
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", false, fmt.Errorf("github app: %s returned %d: %s", path, resp.StatusCode, msg)
	}
	var body struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", false, fmt.Errorf("github app: decoding installation token: %w", err)
	}
	if body.Token == "" {
		return "", false, fmt.Errorf("github app: %s returned no token", path)
	}
	a.mu.Lock()
	if a.tokens == nil {
		a.tokens = map[int64]instToken{}
	}
	a.tokens[installationID] = instToken{value: body.Token, expiresAt: body.ExpiresAt}
	a.mu.Unlock()
	return body.Token, true, nil
}

// invalidate drops the cached token for the installation.
func (a *App) invalidate(installationID int64) {
	a.mu.Lock()
	delete(a.tokens, installationID)
	a.mu.Unlock()
}

// tokenValid probes a cached token against GET /rate_limit — the one
// endpoint that answers cheaply, does not count against the limit, and
// 401s for a token GitHub has revoked server-side. Fail-open on
// transport errors: a network blip must not take the forge surface down
// here when the actual calls would surface it anyway.
func (a *App) tokenValid(ctx context.Context, token string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL()+"/rate_limit", nil)
	if err != nil {
		return true
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := a.client().Do(req)
	if err != nil {
		return true
	}
	resp.Body.Close()
	return resp.StatusCode != http.StatusUnauthorized
}

// ForgeClient returns a Client authenticated as the installation — the
// server's top credential-precedence link (D4). The token is minted (or
// reused) here rather than per request: an upload's handful of API calls
// completes well inside the token's hour, and a mint failure then
// surfaces before any call is attempted. A cache hit is probed first:
// uninstalling revokes tokens immediately, which clock-based expiry
// cannot see — without the probe, the first post-uninstall upload would
// report auth errors instead of degrading like missing credentials.
func (a *App) ForgeClient(ctx context.Context, installationID int64) (forge.Forge, error) {
	token, fresh, err := a.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	if !fresh && !a.tokenValid(ctx, token) {
		a.invalidate(installationID)
		if token, _, err = a.installationToken(ctx, installationID); err != nil {
			return nil, err
		}
	}
	return &Client{BaseURL: a.baseURL(), Token: token, HTTPClient: a.client()}, nil
}

// InstallationAccount returns the login of the org or user account the
// installation lives on. The setup redirect carries only an untrusted
// installation_id; this call is what proves which workspace the install
// belongs to.
func (a *App) InstallationAccount(ctx context.Context, installationID int64) (string, error) {
	var body struct {
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
	}
	path := fmt.Sprintf("/app/installations/%d", installationID)
	if err := a.getJWT(ctx, path, &body); err != nil {
		return "", err
	}
	if body.Account.Login == "" {
		return "", fmt.Errorf("github app: installation %d has no account login", installationID)
	}
	return body.Account.Login, nil
}

// InstallURL returns the app's public install page
// (https://github.com/apps/<slug>/installations/new). The slug is not
// part of the configuration, so it is resolved once via GET /app and
// cached for the process lifetime — app slugs do not change.
func (a *App) InstallURL(ctx context.Context) (string, error) {
	a.mu.Lock()
	cached := a.installURL
	a.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	var body struct {
		HTMLURL string `json:"html_url"`
	}
	if err := a.getJWT(ctx, "/app", &body); err != nil {
		return "", err
	}
	if body.HTMLURL == "" {
		return "", fmt.Errorf("github app: GET /app returned no html_url")
	}
	u := body.HTMLURL + "/installations/new"
	a.mu.Lock()
	a.installURL = u
	a.mu.Unlock()
	return u, nil
}

// getJWT performs a GET authenticated as the app itself (JWT, not an
// installation token). 401/404 map to ErrCredentialsRevoked like the mint.
func (a *App) getJWT(ctx context.Context, path string, out any) error {
	jwt, err := a.appJWT(time.Now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL()+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := a.client().Do(req)
	if err != nil {
		return fmt.Errorf("github app: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%w: github app: %s returned %d: %s",
			forge.ErrCredentialsRevoked, path, resp.StatusCode, msg)
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("github app: %s returned %d: %s", path, resp.StatusCode, msg)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("github app: decoding %s response: %w", path, err)
	}
	return nil
}
