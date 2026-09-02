package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// issuerServer is a fake OIDC issuer: it serves discovery and a JWKS built
// from a test key, and mints tokens signed with it.
type issuerServer struct {
	*httptest.Server
	key *rsa.PrivateKey
	kid string
}

func newIssuer(t *testing.T) *issuerServer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	is := &issuerServer{key: key, kid: "test-key-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": is.URL, "jwks_uri": is.URL + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(is.jwks())
	})
	is.Server = httptest.NewServer(mux)
	t.Cleanup(is.Close)
	return is
}

func (is *issuerServer) jwks() map[string]any {
	return map[string]any{"keys": []map[string]string{{
		"kty": "RSA",
		"kid": is.kid,
		"n":   base64.RawURLEncoding.EncodeToString(is.key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(is.key.E)).Bytes()),
	}}}
}

// mint signs a token with the given header/claims, letting a test override
// any piece.
func (is *issuerServer) mint(t *testing.T, kid, alg string, claims map[string]any) string {
	t.Helper()
	header := map[string]string{"alg": alg, "kid": kid, "typ": "JWT"}
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

// goodClaims is a valid claim set for the issuer and audience.
func (is *issuerServer) goodClaims(aud string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":        is.URL,
		"sub":        "repo:acme/widgets:ref:refs/heads/main",
		"aud":        aud,
		"exp":        now.Add(5 * time.Minute).Unix(),
		"iat":        now.Unix(),
		"nbf":        now.Unix(),
		"repository": "acme/widgets",
	}
}

func newVerifier(is *issuerServer, aud string) *Verifier {
	return New(Config{Audience: aud, Issuers: []string{is.URL}})
}

func TestVerifyOK(t *testing.T) {
	is := newIssuer(t)
	v := newVerifier(is, "https://gocov.example")
	tok := is.mint(t, is.kid, "RS256", is.goodClaims("https://gocov.example"))

	got, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Issuer != is.URL {
		t.Errorf("issuer = %q, want %q", got.Issuer, is.URL)
	}
	if got.Claim("repository") != "acme/widgets" {
		t.Errorf("repository claim = %q", got.Claim("repository"))
	}
	if got.Claim("missing") != "" {
		t.Errorf("missing claim = %q, want empty", got.Claim("missing"))
	}
}

func TestVerifyAudienceArray(t *testing.T) {
	is := newIssuer(t)
	v := newVerifier(is, "https://gocov.example")
	claims := is.goodClaims("")
	claims["aud"] = []string{"someone-else", "https://gocov.example"}
	tok := is.mint(t, is.kid, "RS256", claims)

	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("array audience rejected: %v", err)
	}
}

func TestVerifyRejections(t *testing.T) {
	is := newIssuer(t)
	aud := "https://gocov.example"

	tests := []struct {
		name    string
		claims  func() map[string]any
		alg     string
		kid     string
		wantErr error
	}{
		{
			name:    "bad audience",
			claims:  func() map[string]any { return is.goodClaims("https://other.example") },
			wantErr: ErrBadAudience,
		},
		{
			name: "unknown issuer",
			claims: func() map[string]any {
				c := is.goodClaims(aud)
				c["iss"] = "https://evil.example"
				return c
			},
			wantErr: ErrUnknownIssuer,
		},
		{
			name: "expired",
			claims: func() map[string]any {
				c := is.goodClaims(aud)
				c["exp"] = time.Now().Add(-10 * time.Minute).Unix()
				return c
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "no expiry",
			claims: func() map[string]any {
				c := is.goodClaims(aud)
				delete(c, "exp")
				return c
			},
			wantErr: ErrInvalidToken,
		},
		{
			name:    "unknown kid",
			claims:  func() map[string]any { return is.goodClaims(aud) },
			kid:     "no-such-kid",
			wantErr: ErrInvalidToken,
		},
		{
			name:    "unsupported alg",
			claims:  func() map[string]any { return is.goodClaims(aud) },
			alg:     "HS256",
			wantErr: ErrInvalidToken,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newVerifier(is, aud)
			alg, kid := "RS256", is.kid
			if tt.alg != "" {
				alg = tt.alg
			}
			if tt.kid != "" {
				kid = tt.kid
			}
			tok := is.mint(t, kid, alg, tt.claims())
			_, err := v.Verify(context.Background(), tok)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// A token signed by a different key than the issuer publishes is invalid,
// even with every claim correct.
func TestVerifyBadSignature(t *testing.T) {
	is := newIssuer(t)
	v := newVerifier(is, "https://gocov.example")

	// Sign with a foreign key but keep the issuer's advertised kid.
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	forged := &issuerServer{key: other, kid: is.kid, Server: is.Server}
	tok := forged.mint(t, is.kid, "RS256", is.goodClaims("https://gocov.example"))

	if _, err := v.Verify(context.Background(), tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyMalformed(t *testing.T) {
	v := New(Config{Audience: "https://gocov.example", Issuers: []string{"https://iss.example"}})
	for _, raw := range []string{"", "not-a-jwt", "a.b", "a.b.c.d", "@.@.@"} {
		if _, err := v.Verify(context.Background(), raw); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify(%q) err = %v, want ErrInvalidToken", raw, err)
		}
	}
}

// The verifier caches the JWKS: repeated verifies of a known kid do not
// refetch, and a kid rotated in after the throttle window is picked up.
func TestVerifyCachesAndRotates(t *testing.T) {
	is := newIssuer(t)
	var fetches int
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": is.URL, "jwks_uri": is.URL + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		fetches++
		_ = json.NewEncoder(w).Encode(is.jwks())
	})
	is.Server.Config.Handler = mux

	base := time.Now()
	clock := base
	v := New(Config{Audience: "https://gocov.example", Issuers: []string{is.URL}, Now: func() time.Time { return clock }})

	mintNow := func() string {
		c := is.goodClaims("https://gocov.example")
		c["exp"] = clock.Add(5 * time.Minute).Unix()
		c["iat"] = clock.Unix()
		c["nbf"] = clock.Unix()
		return is.mint(t, is.kid, "RS256", c)
	}
	for range 3 {
		if _, err := v.Verify(context.Background(), mintNow()); err != nil {
			t.Fatal(err)
		}
	}
	if fetches != 1 {
		t.Errorf("jwks fetched %d times, want 1 (cached)", fetches)
	}

	// The issuer rotates to a new kid. Past the throttle window the unknown
	// kid triggers exactly one refetch, after which the new token verifies.
	is.kid = "test-key-2"
	clock = base.Add(2 * time.Minute)
	if _, err := v.Verify(context.Background(), mintNow()); err != nil {
		t.Fatalf("after rotation: %v", err)
	}
	if fetches != 2 {
		t.Errorf("jwks fetched %d times, want 2 after rotation", fetches)
	}
}

// A discovery document that claims a different issuer than the one asked
// for is refused, so a substituted endpoint cannot redirect the key fetch.
// The failure is transient (not a token verdict), so a caller maps it to a
// gateway error, not a rejection.
func TestVerifyDiscoveryIssuerMismatch(t *testing.T) {
	is := newIssuer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": "https://evil.example", "jwks_uri": is.URL + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(is.jwks())
	})
	is.Server.Config.Handler = mux

	v := newVerifier(is, "https://gocov.example")
	_, err := v.Verify(context.Background(), is.mint(t, is.kid, "RS256", is.goodClaims("https://gocov.example")))
	if err == nil {
		t.Fatal("mismatched discovery issuer accepted")
	}
	if errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrBadAudience) || errors.Is(err, ErrUnknownIssuer) {
		t.Fatalf("err = %v, want a transient (non-verdict) error", err)
	}
}

// An issuer not on the exact allowlist is accepted when the matcher admits
// it (Bitbucket's per-workspace issuer), and rejected when it does not.
func TestVerifyIssuerMatcher(t *testing.T) {
	is := newIssuer(t)
	// No exact issuers; only the matcher, which admits this test issuer.
	v := New(Config{
		Audience:    "https://gocov.example",
		IssuerMatch: func(iss string) bool { return iss == is.URL },
	})
	if _, err := v.Verify(context.Background(), is.mint(t, is.kid, "RS256", is.goodClaims("https://gocov.example"))); err != nil {
		t.Fatalf("matcher-admitted issuer rejected: %v", err)
	}

	// A different issuer the matcher does not admit is unknown.
	claims := is.goodClaims("https://gocov.example")
	claims["iss"] = "https://api.bitbucket.org/2.0/workspaces/evil/pipelines-config/identity/oidc"
	if _, err := v.Verify(context.Background(), is.mint(t, is.kid, "RS256", claims)); !errors.Is(err, ErrUnknownIssuer) {
		t.Fatalf("err = %v, want ErrUnknownIssuer", err)
	}
}

// An unknown kid does not trigger a fresh fetch on every attempt: within the
// throttle window the second miss is answered from cache.
func TestUnknownKidThrottled(t *testing.T) {
	is := newIssuer(t)
	var fetches int
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": is.URL, "jwks_uri": is.URL + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		fetches++
		_ = json.NewEncoder(w).Encode(is.jwks())
	})
	is.Server.Config.Handler = mux

	v := newVerifier(is, "https://gocov.example")
	for range 3 {
		tok := is.mint(t, "bogus-kid", "RS256", is.goodClaims("https://gocov.example"))
		if _, err := v.Verify(context.Background(), tok); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("err = %v, want ErrInvalidToken", err)
		}
	}
	if fetches != 1 {
		t.Errorf("jwks fetched %d times for a bogus kid, want 1 (throttled)", fetches)
	}
}
