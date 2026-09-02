// Package oidc verifies the OIDC identity tokens a CI runner mints for
// itself, so an upload can prove which repository it came from without a
// pasted secret. A workflow with the forge's "id-token" permission asks the
// forge for a short-lived JWT bound to gocov's audience; this package checks
// that JWT the way any OIDC relying party does — issuer on an allowlist,
// signature valid against the issuer's published keys, audience ours, not
// expired — and hands the caller the claims to map to a tracked repo.
//
// It is stdlib-only (crypto/rsa, encoding/json) and forge-agnostic: the
// generic protocol lives here, while which claim names a repository is the
// caller's business (GitHub's "repository", GitLab's "project_path",
// Bitbucket's UUID-bearing "sub"). One Verifier serves the whole process,
// caching each issuer's key set across requests; it is safe for concurrent
// use.
package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The verification verdicts a caller distinguishes to answer with the right
// status and reason. Everything else a Verify can return — a JWKS that would
// not fetch, an issuer that would not answer discovery — is transient and
// wraps none of these: the caller retries or reports a gateway error.
var (
	// ErrUnknownIssuer means the token's iss is not on the allowlist. It is
	// decided before any network call, so an unrecognized issuer never makes
	// us fetch anything.
	ErrUnknownIssuer = errors.New("oidc: issuer not allowed")
	// ErrBadAudience means the token was minted for someone else — its aud
	// is not this server. This is the check that stops a token leaked from
	// another service being replayed at gocov.
	ErrBadAudience = errors.New("oidc: audience mismatch")
	// ErrInvalidToken covers a token that is malformed, signed with an
	// unknown or unsupported key, or outside its validity window — anything
	// that means "this is not a genuine, current token from the issuer".
	ErrInvalidToken = errors.New("oidc: invalid token")
)

// leeway forgives small clock skew between the forge and this server on the
// time-bound claims.
const leeway = 60 * time.Second

// refetchThrottle bounds how often an unknown key id triggers a fresh JWKS
// fetch, so a token naming a bogus kid cannot make us hammer the issuer.
// Key rotation publishes the new kid ahead of use, so one fetch per window
// is always enough to catch up.
const refetchThrottle = time.Minute

// Token is a verified identity token: the standard fields the protocol
// needs, plus the raw claim set for the caller to read the forge-specific
// repository claim from.
type Token struct {
	Issuer   string
	Subject  string
	Audience []string

	claims map[string]json.RawMessage
}

// Claim returns a string-valued claim, or "" when it is absent or not a
// string. The repository-identity claims every forge carries (GitHub's
// "repository", GitLab's "project_path") are plain strings.
func (t *Token) Claim(name string) string {
	raw, ok := t.claims[name]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// Verifier checks tokens against a fixed audience and issuer allowlist,
// caching each issuer's signing keys.
type Verifier struct {
	audience string
	issuers  map[string]bool
	match    func(string) bool
	http     *http.Client
	now      func() time.Time

	mu   sync.Mutex
	keys map[string]*keySet // by issuer
}

// keySet is one issuer's cached signing keys and when they were fetched.
type keySet struct {
	byKID     map[string]*rsa.PublicKey
	fetchedAt time.Time
}

// Config configures a Verifier. Audience and Issuers are required;
// HTTPClient and Now default to a short-timeout client and time.Now, and
// are settable so tests can point at a local issuer with a controlled clock.
type Config struct {
	// Audience is the value a token's aud must carry — this server's public
	// URL. A token minted for any other audience is refused.
	Audience string
	// Issuers is the allowlist of exact trusted iss values (e.g. GitHub
	// Actions' single issuer). A token from anything else is refused
	// without a fetch, unless IssuerMatch admits it.
	Issuers []string
	// IssuerMatch optionally admits issuers that are not a fixed string —
	// Bitbucket mints a per-workspace issuer, so its whole family is
	// recognized by shape (fixed host, fixed path template) rather than
	// enumerated. It runs only when the exact allowlist misses, and must
	// itself pin the scheme and host: whatever it admits, this package will
	// fetch discovery from. Nil means exact matches only.
	IssuerMatch func(issuer string) bool
	HTTPClient  *http.Client
	Now         func() time.Time
}

// New builds a Verifier. It panics on an empty audience, or when neither an
// issuer allowlist nor an issuer matcher is given — a misconfiguration that
// would silently accept or reject everything.
func New(cfg Config) *Verifier {
	if strings.TrimSpace(cfg.Audience) == "" {
		panic("oidc: audience is required")
	}
	if len(cfg.Issuers) == 0 && cfg.IssuerMatch == nil {
		panic("oidc: at least one issuer or an issuer matcher is required")
	}
	issuers := make(map[string]bool, len(cfg.Issuers))
	for _, iss := range cfg.Issuers {
		issuers[strings.TrimRight(iss, "/")] = true
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Verifier{
		audience: strings.TrimRight(cfg.Audience, "/"),
		issuers:  issuers,
		match:    cfg.IssuerMatch,
		http:     httpClient,
		now:      now,
		keys:     map[string]*keySet{},
	}
}

// issuerAllowed reports whether the issuer is trusted: on the exact
// allowlist, or admitted by the matcher.
func (v *Verifier) issuerAllowed(issuer string) bool {
	return v.issuers[issuer] || (v.match != nil && v.match(issuer))
}

// Verify checks a raw compact-JWT identity token and returns its claims.
// The order is deliberate: reject an unknown issuer before any network
// call, then prove the signature against the issuer's key, then the time
// window, and only then the audience — so a forged token fails as invalid
// rather than leaking which audience it should have named.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Token, error) {
	header, payload, signingInput, sig, err := split(raw)
	if err != nil {
		return nil, err
	}

	var hdr struct {
		Alg string `json:"alg"`
		KID string `json:"kid"`
	}
	if err := json.Unmarshal(header, &hdr); err != nil {
		return nil, fmt.Errorf("%w: header is not JSON", ErrInvalidToken)
	}
	// RS256 is what all three forge issuers mint; refusing everything else
	// keeps "alg" from being an attacker's choice (e.g. a downgrade).
	if hdr.Alg != "RS256" {
		return nil, fmt.Errorf("%w: unsupported signing algorithm %q", ErrInvalidToken, hdr.Alg)
	}

	var claims struct {
		Issuer    string          `json:"iss"`
		Subject   string          `json:"sub"`
		Audience  json.RawMessage `json:"aud"`
		Expiry    int64           `json:"exp"`
		IssuedAt  int64           `json:"iat"`
		NotBefore int64           `json:"nbf"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("%w: payload is not JSON", ErrInvalidToken)
	}

	issuer := strings.TrimRight(claims.Issuer, "/")
	if !v.issuerAllowed(issuer) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownIssuer, claims.Issuer)
	}

	key, err := v.publicKey(ctx, issuer, hdr.KID)
	if err != nil {
		return nil, err
	}
	hashed := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, hashed[:], sig); err != nil {
		return nil, fmt.Errorf("%w: signature does not verify", ErrInvalidToken)
	}

	now := v.now()
	if claims.Expiry == 0 {
		return nil, fmt.Errorf("%w: token has no expiry", ErrInvalidToken)
	}
	if now.After(time.Unix(claims.Expiry, 0).Add(leeway)) {
		return nil, fmt.Errorf("%w: token expired", ErrInvalidToken)
	}
	if claims.NotBefore != 0 && now.Before(time.Unix(claims.NotBefore, 0).Add(-leeway)) {
		return nil, fmt.Errorf("%w: token not yet valid", ErrInvalidToken)
	}
	if claims.IssuedAt != 0 && now.Before(time.Unix(claims.IssuedAt, 0).Add(-leeway)) {
		return nil, fmt.Errorf("%w: token issued in the future", ErrInvalidToken)
	}

	auds, err := audiences(claims.Audience)
	if err != nil {
		return nil, err
	}
	if !contains(auds, v.audience) {
		return nil, fmt.Errorf("%w: aud %v is not %q", ErrBadAudience, auds, v.audience)
	}

	var all map[string]json.RawMessage
	if err := json.Unmarshal(payload, &all); err != nil {
		return nil, fmt.Errorf("%w: payload is not JSON", ErrInvalidToken)
	}
	return &Token{
		Issuer:   issuer,
		Subject:  claims.Subject,
		Audience: auds,
		claims:   all,
	}, nil
}

// split breaks a compact JWT into its decoded header and payload, the ASCII
// signing input the signature covers, and the decoded signature.
func split(raw string) (header, payload []byte, signingInput string, sig []byte, err error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, nil, "", nil, fmt.Errorf("%w: want three dot-separated segments", ErrInvalidToken)
	}
	if header, err = b64(parts[0]); err != nil {
		return nil, nil, "", nil, fmt.Errorf("%w: header is not base64url", ErrInvalidToken)
	}
	if payload, err = b64(parts[1]); err != nil {
		return nil, nil, "", nil, fmt.Errorf("%w: payload is not base64url", ErrInvalidToken)
	}
	if sig, err = b64(parts[2]); err != nil {
		return nil, nil, "", nil, fmt.Errorf("%w: signature is not base64url", ErrInvalidToken)
	}
	return header, payload, parts[0] + "." + parts[1], sig, nil
}

// b64 decodes base64url, tolerating tokens that carry padding even though
// the JWT spec omits it.
func b64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// audiences normalizes the aud claim, which JWT allows to be either a single
// string or an array of them.
func audiences(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: token has no audience", ErrBadAudience)
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}, nil
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many, nil
	}
	return nil, fmt.Errorf("%w: audience is neither string nor array", ErrInvalidToken)
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if strings.TrimRight(s, "/") == want {
			return true
		}
	}
	return false
}

// publicKey returns the issuer's RSA key for the given kid, fetching and
// caching the issuer's JWKS on a miss. An unknown kid triggers at most one
// fetch per throttle window (rotation publishes new kids ahead of use); a
// still-missing kid after a fresh fetch is a bad token, a fetch failure is
// transient.
func (v *Verifier) publicKey(ctx context.Context, issuer, kid string) (*rsa.PublicKey, error) {
	if kid == "" {
		return nil, fmt.Errorf("%w: no key id in header", ErrInvalidToken)
	}
	if key, fresh := v.cached(issuer, kid); key != nil {
		return key, nil
	} else if fresh {
		// The set was fetched within the throttle window and still lacks this
		// kid — treat it as a bad token rather than re-fetching on demand.
		return nil, fmt.Errorf("%w: unknown key id %q", ErrInvalidToken, kid)
	}

	set, err := v.fetchKeys(ctx, issuer)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	v.keys[issuer] = set
	v.mu.Unlock()

	if key := set.byKID[kid]; key != nil {
		return key, nil
	}
	return nil, fmt.Errorf("%w: unknown key id %q", ErrInvalidToken, kid)
}

// cached returns the cached key for the kid, and whether the cached set is
// fresh enough that a miss should not trigger a refetch.
func (v *Verifier) cached(issuer, kid string) (key *rsa.PublicKey, fresh bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	set := v.keys[issuer]
	if set == nil {
		return nil, false
	}
	if key := set.byKID[kid]; key != nil {
		return key, true
	}
	return nil, v.now().Sub(set.fetchedAt) < refetchThrottle
}

// fetchKeys resolves the issuer's JWKS through OIDC discovery and parses its
// RSA keys.
func (v *Verifier) fetchKeys(ctx context.Context, issuer string) (*keySet, error) {
	var disco struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := v.getJSON(ctx, issuer+"/.well-known/openid-configuration", &disco); err != nil {
		return nil, fmt.Errorf("oidc: discovery for %s: %w", issuer, err)
	}
	// The discovery document must claim the issuer we asked for (OpenID
	// Connect Discovery §4.3). A mismatch means the document was substituted
	// — a misconfigured or hijacked endpoint pointing our key fetch at keys
	// that belong to someone else — so refuse it rather than trust its
	// jwks_uri.
	if strings.TrimRight(disco.Issuer, "/") != issuer {
		return nil, fmt.Errorf("oidc: discovery for %s claims issuer %q", issuer, disco.Issuer)
	}
	if disco.JWKSURI == "" {
		return nil, fmt.Errorf("oidc: discovery for %s has no jwks_uri", issuer)
	}
	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := v.getJSON(ctx, disco.JWKSURI, &jwks); err != nil {
		return nil, fmt.Errorf("oidc: fetching jwks for %s: %w", issuer, err)
	}
	set := &keySet{byKID: map[string]*rsa.PublicKey{}, fetchedAt: v.now()}
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		key, err := rsaKey(k.N, k.E)
		if err != nil {
			continue // skip a malformed key, keep the rest
		}
		set.byKID[k.Kid] = key
	}
	if len(set.byKID) == 0 {
		return nil, fmt.Errorf("oidc: jwks for %s has no usable RSA keys", issuer)
	}
	return set, nil
}

// rsaKey builds an RSA public key from a JWK's base64url modulus and
// exponent.
func rsaKey(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := b64(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := b64(eB64)
	if err != nil {
		return nil, err
	}
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	if e == 0 {
		return nil, errors.New("zero exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

// getJSON fetches a URL and decodes a JSON body, bounding both the response
// size and (via the caller's context) the time.
func (v *Verifier) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("decoding %s: %w", url, err)
	}
	return nil
}
