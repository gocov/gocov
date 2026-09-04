package github

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/forge"
)

// testKey is generated once; RSA key generation dominates test time
// otherwise.
var testKey = func() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
}()

func testKeyPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(testKey),
	})
}

func testApp(t *testing.T, handler http.HandlerFunc) *App {
	t.Helper()
	app, err := NewApp("1234", testKeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	if handler != nil {
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)
		app.BaseURL = srv.URL
		app.HTTPClient = srv.Client()
	}
	return app
}

func TestNewAppParsesKeys(t *testing.T) {
	if _, err := NewApp("1234", testKeyPEM()); err != nil {
		t.Errorf("PKCS#1 key rejected: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(testKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewApp("1234", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})); err != nil {
		t.Errorf("PKCS#8 key rejected: %v", err)
	}
	if _, err := NewApp("1234", []byte("not a key")); err == nil {
		t.Error("garbage key accepted")
	}
	if _, err := NewApp("", testKeyPEM()); err == nil {
		t.Error("empty app id accepted")
	}
}

func TestAppJWT(t *testing.T) {
	app := testApp(t, nil)
	now := time.Now()
	token, err := app.appJWT(now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts, want 3", len(parts))
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&testKey.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}

	var header struct{ Alg, Typ string }
	decodeSegment(t, parts[0], &header)
	if header.Alg != "RS256" || header.Typ != "JWT" {
		t.Errorf("header = %+v, want RS256/JWT", header)
	}
	var claims struct {
		Iat, Exp int64
		Iss      string
	}
	decodeSegment(t, parts[1], &claims)
	if claims.Iss != "1234" {
		t.Errorf("iss = %q, want 1234", claims.Iss)
	}
	if got := claims.Iat; got != now.Add(-time.Minute).Unix() {
		t.Errorf("iat = %d, want backdated one minute", got)
	}
	if lifetime := claims.Exp - claims.Iat; lifetime > 10*60 {
		t.Errorf("jwt lives %ds, over GitHub's 10-minute cap", lifetime)
	}
}

func decodeSegment(t *testing.T, seg string, dst any) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatal(err)
	}
}

// mintHandler answers the token mint (and any later API call made with
// the minted token), recording how often the mint ran.
func mintHandler(t *testing.T, expiresIn time.Duration, mints *int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer eyJ") {
				t.Errorf("mint auth = %q, want a Bearer JWT", r.Header.Get("Authorization"))
			}
			*mints++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      fmt.Sprintf("itok-%d", *mints),
				"expires_at": time.Now().Add(expiresIn),
			})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("{}"))
	}
}

func TestInstallationTokenCached(t *testing.T) {
	mints := 0
	app := testApp(t, mintHandler(t, time.Hour, &mints))

	for range 3 {
		tok, _, err := app.installationToken(t.Context(), 42)
		if err != nil {
			t.Fatal(err)
		}
		if tok != "itok-1" {
			t.Fatalf("token = %q, want the first mint reused", tok)
		}
	}
	if mints != 1 {
		t.Errorf("minted %d times for 3 calls, want 1", mints)
	}
}

func TestInstallationTokenRefreshesNearExpiry(t *testing.T) {
	// Expires within the leeway window, so every call must re-mint.
	mints := 0
	app := testApp(t, mintHandler(t, time.Minute, &mints))

	for range 2 {
		if _, _, err := app.installationToken(t.Context(), 42); err != nil {
			t.Fatal(err)
		}
	}
	if mints != 2 {
		t.Errorf("minted %d times, want 2 (near-expiry token must not be reused)", mints)
	}
}

func TestInstallationTokenRevoked(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound} {
		app := testApp(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"Not Found"}`, status)
		})
		_, _, err := app.installationToken(t.Context(), 42)
		if !errors.Is(err, forge.ErrCredentialsRevoked) {
			t.Errorf("status %d: err = %v, want ErrCredentialsRevoked", status, err)
		}
	}
}

func TestInstallationTokenServerError(t *testing.T) {
	app := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, _, err := app.installationToken(t.Context(), 42)
	if err == nil || errors.Is(err, forge.ErrCredentialsRevoked) {
		t.Errorf("err = %v, want a plain (transient) error", err)
	}
}

func TestForgeClientUsesInstallationToken(t *testing.T) {
	mints := 0
	var gotAuth string
	app := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			mintHandler(t, time.Hour, &mints)(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("{}"))
	})

	fg, err := app.ForgeClient(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	err = fg.PostBuildStatus(t.Context(), "acme/widgets", "abc", forge.BuildStatus{
		State: forge.StateSuccessful, Name: "gocov",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer itok-1" {
		t.Errorf("API call auth = %q, want the minted installation token", gotAuth)
	}
}

func TestInstallationAccount(t *testing.T) {
	app := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/installations/42" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer eyJ") {
			t.Errorf("auth = %q, want a Bearer JWT", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"account":{"login":"acme"}}`))
	})
	login, err := app.InstallationAccount(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if login != "acme" {
		t.Errorf("login = %q, want acme", login)
	}
}

func TestInstallationAccountNotFound(t *testing.T) {
	app := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	_, err := app.InstallationAccount(t.Context(), 42)
	if !errors.Is(err, forge.ErrCredentialsRevoked) {
		t.Errorf("err = %v, want ErrCredentialsRevoked", err)
	}
}

func TestInstallURLCached(t *testing.T) {
	calls := 0
	app := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app" {
			t.Errorf("path = %s", r.URL.Path)
		}
		calls++
		_, _ = w.Write([]byte(`{"html_url":"https://github.com/apps/gocov"}`))
	})
	for range 2 {
		u, err := app.InstallURL(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if u != "https://github.com/apps/gocov/installations/new" {
			t.Errorf("url = %q", u)
		}
	}
	if calls != 1 {
		t.Errorf("GET /app ran %d times, want 1 (cached)", calls)
	}
}

func TestForgeClientProbesCachedToken(t *testing.T) {
	// Uninstall revokes tokens immediately; clock-based expiry cannot
	// see that. A cache hit must be probed, and a revoked probe must
	// surface as ErrCredentialsRevoked via a fresh mint attempt — not as
	// 401s from the actual API calls later.
	mints, probes := 0, 0
	app := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			mints++
			if mints > 1 { // the re-mint after the failed probe: gone
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "itok-1", "expires_at": time.Now().Add(time.Hour),
			})
		case r.URL.Path == "/rate_limit":
			probes++
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		default:
			t.Errorf("unexpected call %s", r.URL.Path)
		}
	})

	// First client: fresh mint, no probe.
	if _, err := app.ForgeClient(t.Context(), 42); err != nil {
		t.Fatal(err)
	}
	if probes != 0 {
		t.Errorf("fresh mint was probed %d times, want 0", probes)
	}
	// Second client: cache hit -> failed probe -> re-mint -> revoked.
	_, err := app.ForgeClient(t.Context(), 42)
	if !errors.Is(err, forge.ErrCredentialsRevoked) {
		t.Fatalf("err = %v, want ErrCredentialsRevoked", err)
	}
	if probes != 1 || mints != 2 {
		t.Errorf("probes = %d, mints = %d; want 1 probe and a re-mint", probes, mints)
	}
}

func TestForgeClientValidCachedToken(t *testing.T) {
	mints, probes := 0, 0
	app := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			mintHandler(t, time.Hour, &mints)(w, r)
		case r.URL.Path == "/rate_limit":
			probes++
			_, _ = w.Write([]byte(`{}`))
		}
	})
	for range 2 {
		if _, err := app.ForgeClient(t.Context(), 42); err != nil {
			t.Fatal(err)
		}
	}
	if mints != 1 || probes != 1 {
		t.Errorf("mints = %d, probes = %d; want one mint and one cache-hit probe", mints, probes)
	}
}
