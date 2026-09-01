package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	forgefake "github.com/gocov/gocov/internal/forge/fake"
	"github.com/gocov/gocov/internal/oidc"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// oidcTestKey signs test identity tokens; its public half is served as the
// GitHub issuer's JWKS through the rewriting client below.
type oidcIssuer struct {
	key *rsa.PrivateKey
	kid string
}

func newOIDCIssuer(t *testing.T) (*oidcIssuer, *http.Client) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	is := &oidcIssuer{key: key, kid: "k1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		// Advertise the JWKS on the real issuer host; the rewriting transport
		// sends the fetch to this test server.
		_ = json.NewEncoder(w).Encode(map[string]string{"jwks_uri": gitHubActionsIssuer + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA",
			"kid": is.kid,
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: rewriteHost{target: srv.Listener.Addr().String()}}
	return is, client
}

// rewriteHost sends every request to the test JWKS server, whatever host the
// verifier addressed — so a token's iss can stay the real GitHub issuer
// (which the claim mapping keys off) while the fetch stays local.
type rewriteHost struct{ target string }

func (rt rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = rt.target
	return http.DefaultTransport.RoundTrip(req)
}

func (is *oidcIssuer) mint(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]string{"alg": "RS256", "kid": is.kid, "typ": "JWT"}
	seg := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signingInput := seg(header) + "." + seg(claims)
	hashed := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, is.key, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// githubClaims is a valid GitHub Actions identity token for acme/widgets.
func githubClaims(aud string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":        gitHubActionsIssuer,
		"sub":        "repo:acme/widgets:ref:refs/heads/main",
		"aud":        aud,
		"exp":        now.Add(5 * time.Minute).Unix(),
		"iat":        now.Unix(),
		"repository": "acme/widgets",
	}
}

// newOIDCFixture builds a github-forge server with repo acme/widgets whose
// workspace is App-connected, and an OIDC verifier bound to the fixture's
// BaseURL as audience and pointed at the local test issuer.
func newOIDCFixture(t *testing.T) (*fixture, *oidcIssuer) {
	t.Helper()
	ctx := context.Background()
	st := storemem.New()
	repo := &store.Repo{Forge: "github", Slug: "acme/widgets", Token: "secret-token", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	ws := &store.Workspace{Forge: "github", Prefix: "acme", Token: "ws-secret", DefaultBranch: "main", GitHubInstallationID: 77}
	if err := st.CreateWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	blobs := blobmem.New()
	ff := forgefake.New()
	app := &fakeGitHubApp{appForge: ff, accounts: map[int64]string{77: "acme"}}

	is, client := newOIDCIssuer(t)
	verifier := oidc.New(oidc.Config{
		Audience:   "https://gocov.example",
		Issuers:    []string{gitHubActionsIssuer},
		HTTPClient: client,
	})
	srv := New(Config{
		Store:        st,
		Blobs:        blobs,
		Parsers:      map[string]profile.Parser{"go": profile.GoParser{}},
		BaseURL:      "https://gocov.example",
		GitHubApp:    app,
		OIDCVerifier: verifier,
	})
	return &fixture{srv: srv, store: st, blobs: blobs, forge: ff, repo: repo}, is
}

// doOIDCUpload posts an upload authenticated by an OIDC token (no bearer,
// no run claim).
func doOIDCUpload(t *testing.T, f *fixture, token string, extra map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	fields := map[string]string{
		"repo":       "acme/widgets",
		"commit":     "abc123",
		"branch":     "main",
		"oidc_token": token,
	}
	for k, v := range extra {
		if v == "" {
			delete(fields, k)
			continue
		}
		fields[k] = v
	}
	return doUpload(t, f, "", fields, testProfile)
}

func TestOIDCUploadHappyPath(t *testing.T) {
	f, is := newOIDCFixture(t)
	tok := is.mint(t, githubClaims("https://gocov.example"))

	rec := doOIDCUpload(t, f, tok, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	// A forge-signed identity is a fully verified upload — not the fork-PR
	// "unverified" mark.
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	u, err := f.store.Upload(context.Background(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Meta.Tokenless {
		t.Error("OIDC upload wrongly marked tokenless/unverified")
	}

	// Status posted through the App installation, exactly like a token upload.
	if len(f.forge.StatusCalls) != 1 {
		t.Errorf("got %d status calls, want 1", len(f.forge.StatusCalls))
	}
}

func TestOIDCBadAudience(t *testing.T) {
	f, is := newOIDCFixture(t)
	tok := is.mint(t, githubClaims("https://someone-else.example"))

	rec := doOIDCUpload(t, f, tok, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	assertErrorContains(t, rec, "oidc_bad_audience")
}

func TestOIDCRepoMismatch(t *testing.T) {
	f, is := newOIDCFixture(t)
	tok := is.mint(t, githubClaims("https://gocov.example"))

	// The token is for acme/widgets; the form claims another repo.
	rec := doOIDCUpload(t, f, tok, map[string]string{"repo": "acme/other"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	assertErrorContains(t, rec, "oidc_repo_mismatch")
}

func TestOIDCUntracked(t *testing.T) {
	f, is := newOIDCFixture(t)
	claims := githubClaims("https://gocov.example")
	claims["repository"] = "stranger/repo"
	tok := is.mint(t, claims)

	// The form repo is omitted so the mismatch check does not fire first;
	// the untracked repository is what must be reported.
	rec := doOIDCUpload(t, f, tok, map[string]string{"repo": ""})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestOIDCInvalidToken(t *testing.T) {
	f, _ := newOIDCFixture(t)
	rec := doOIDCUpload(t, f, "not.a.jwt", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	assertErrorContains(t, rec, "oidc_invalid_token")
}

func TestOIDCUnknownIssuer(t *testing.T) {
	f, is := newOIDCFixture(t)
	claims := githubClaims("https://gocov.example")
	claims["iss"] = "https://evil.example"
	tok := is.mint(t, claims)

	rec := doOIDCUpload(t, f, tok, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	assertErrorContains(t, rec, "oidc_unknown_issuer")
}

func assertErrorContains(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if !strings.Contains(body.Error, want) {
		t.Errorf("error %q does not contain %q", body.Error, want)
	}
}

// A server built without a BaseURL has no audience to bind, so OIDC uploads
// are unavailable rather than panicking at construction.
func TestOIDCUnavailableWithoutBaseURL(t *testing.T) {
	srv := New(Config{
		Store:   storemem.New(),
		Blobs:   blobmem.New(),
		Parsers: map[string]profile.Parser{"go": profile.GoParser{}},
	})
	if srv.oidc != nil {
		t.Fatal("verifier built without a BaseURL")
	}
}
