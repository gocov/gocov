package server

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/store"
)

const (
	testRedirect = "https://claude.ai/api/mcp/auth_callback"
	testVerifier = "verifier-verifier-verifier-verifier-verifier-0123456789"
)

func testChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// registerMCPClient registers a client the way claude.ai does and returns
// its id.
func registerMCPClient(t *testing.T, f *fixture, redirectURIs ...string) string {
	t.Helper()
	rec := postJSON(t, f, "/mcp/register", map[string]any{
		"client_name": "Claude", "redirect_uris": redirectURIs,
		"grant_types": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_method": "none",
	})
	wantStatus(t, rec, "register", http.StatusCreated)
	reg := decodeJSON[map[string]any](t, rec)
	id, _ := reg["client_id"].(string)
	if id == "" || reg["token_endpoint_auth_method"] != "none" || reg["client_secret"] != nil {
		t.Fatalf("registration: %v", reg)
	}
	return id
}

// authorizeQuery is the query a client opens the consent page with.
func authorizeQuery(clientID string) url.Values {
	return url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {testRedirect},
		"code_challenge": {testChallenge(testVerifier)}, "code_challenge_method": {"S256"},
		"state": {"st4te"}, "resource": {"https://gocov.example/mcp"},
	}
}

// approve posts the consent page's approval and returns where it sends the
// browser.
func approve(t *testing.T, f *fixture, q url.Values, sess *http.Cookie) *url.URL {
	t.Helper()
	rec := postJSON(t, f, "/api/ui/mcp-authorize/approve", map[string]string{"query": "?" + q.Encode()}, sess)
	wantStatus(t, rec, "approve", http.StatusOK)
	dto := decodeJSON[mcpConsentDTO](t, rec)
	u, err := url.Parse(dto.Redirect)
	if err != nil || dto.Redirect == "" {
		t.Fatalf("approve redirect %q: %v", dto.Redirect, err)
	}
	return u
}

func exchange(f *fixture, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func codeForm(clientID, code, verifier string) url.Values {
	return url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier},
		"client_id": {clientID}, "redirect_uri": {testRedirect},
	}
}

// The whole connector sign-in, as claude.ai walks it: discovery from the
// 401, registration, the consent page signed in as a workspace member,
// the code exchange — and then the member's private repo reads.
func TestMCPSignInReadsAMembersPrivateRepo(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPrivate, true)
	seedCommitReport(t, f, prReport())

	// Signed out, /mcp names its metadata.
	rec := postMCP(f, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	wantStatus(t, rec, "/mcp", http.StatusUnauthorized)
	if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer resource_metadata="https://gocov.example/.well-known/oauth-protected-resource/mcp"` {
		t.Errorf("WWW-Authenticate = %q", got)
	}
	prm := decodeJSON[map[string]any](t, get(f, "/.well-known/oauth-protected-resource/mcp"))
	if prm["resource"] != "https://gocov.example/mcp" || prm["authorization_servers"].([]any)[0] != "https://gocov.example" {
		t.Errorf("resource metadata: %v", prm)
	}
	as := decodeJSON[map[string]any](t, get(f, "/.well-known/oauth-authorization-server"))
	if as["issuer"] != "https://gocov.example" || as["authorization_endpoint"] != "https://gocov.example/mcp/authorize" ||
		as["token_endpoint"] != "https://gocov.example/mcp/token" || as["registration_endpoint"] != "https://gocov.example/mcp/register" {
		t.Errorf("server metadata: %v", as)
	}

	clientID := registerMCPClient(t, f, testRedirect)
	q := authorizeQuery(clientID)

	// The consent page sends a signed-out browser through sign-in and
	// back to itself, query and all.
	page := get(f, "/mcp/authorize?"+q.Encode())
	wantStatus(t, page, "/mcp/authorize", http.StatusFound)
	loginNext, _ := url.Parse(page.Header().Get("Location"))
	sess := signIn(t, f, loginNext.Query().Get("next"))
	page = get(f, "/mcp/authorize?"+q.Encode(), sess)
	wantStatus(t, page, "/mcp/authorize signed in", http.StatusOK)
	if page.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("the consent page may be framed")
	}

	consent := decodeJSON[mcpConsentDTO](t, get(f, "/api/ui/mcp-authorize?"+q.Encode(), sess))
	if consent.Client != "Claude" || consent.RedirectOrigin != "https://claude.ai" || consent.User != "Jane Dev" || consent.Redirect != "" {
		t.Errorf("consent: %+v", consent)
	}
	if deny, _ := url.Parse(consent.DenyURL); deny.Query().Get("error") != "access_denied" || deny.Query().Get("state") != "st4te" {
		t.Errorf("deny URL %q", consent.DenyURL)
	}

	back := approve(t, f, q, sess)
	if got := back.Scheme + "://" + back.Host + back.Path; got != testRedirect ||
		back.Query().Get("state") != "st4te" || back.Query().Get("iss") != "https://gocov.example" {
		t.Errorf("approval redirect %s", back)
	}
	tokRec := exchange(f, codeForm(clientID, back.Query().Get("code"), testVerifier))
	wantStatus(t, tokRec, "token", http.StatusOK)
	tok := decodeJSON[map[string]any](t, tokRec)
	access, _ := tok["access_token"].(string)
	if !strings.HasPrefix(access, mcpTokenPrefix) || tok["token_type"] != "Bearer" {
		t.Fatalf("token: %v", tok)
	}
	if tokRec.Header().Get("Cache-Control") != "no-store" {
		t.Error("token response may be cached")
	}

	text, isErr := callMCP(t, f, access, `{"forge":"bitbucket","repo":"acme/widgets","commit":"feed1234"}`)
	if isErr || !strings.Contains(text, "diff coverage: 85.7%") {
		t.Errorf("member read (error %v): %s", isErr, text)
	}

	// A code is good once.
	if rec := exchange(f, codeForm(clientID, back.Query().Get("code"), testVerifier)); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("second exchange: %d %s", rec.Code, rec.Body)
	}
	// The token reads reports; it is not a session.
	if rec := get(f, "/repos/bitbucket/acme/widgets", &http.Cookie{Name: sessionCookie, Value: access}); rec.Code != http.StatusFound {
		t.Errorf("MCP token as a session cookie: status = %d", rec.Code)
	}
}

func TestMCPTokenExchangeRefusesMismatches(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPrivate, true)
	clientID := registerMCPClient(t, f, testRedirect)
	other := registerMCPClient(t, f, "https://evil.example/cb")
	sess := signIn(t, f, "/")
	for name, form := range map[string]func(code string) url.Values{
		"wrong verifier": func(code string) url.Values {
			return codeForm(clientID, code, strings.Repeat("x", 43))
		},
		"another client": func(code string) url.Values { return codeForm(other, code, testVerifier) },
		"another redirect": func(code string) url.Values {
			f := codeForm(clientID, code, testVerifier)
			f.Set("redirect_uri", "https://claude.ai/other")
			return f
		},
		"another resource": func(code string) url.Values {
			f := codeForm(clientID, code, testVerifier)
			f.Set("resource", "https://elsewhere.example/mcp")
			return f
		},
	} {
		code := approve(t, f, authorizeQuery(clientID), sess).Query().Get("code")
		rec := exchange(f, form(code))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d %s", name, rec.Code, rec.Body)
		}
		// And the code is spent: the right exchange after it fails too.
		if name != "another resource" {
			if rec := exchange(f, codeForm(clientID, code, testVerifier)); rec.Code != http.StatusBadRequest {
				t.Errorf("%s: code survived a failed exchange", name)
			}
		}
	}
	if rec := exchange(f, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}}); !strings.Contains(rec.Body.String(), "unsupported_grant_type") {
		t.Errorf("refresh grant: %d %s", rec.Code, rec.Body)
	}
}

func TestMCPConsentRefusesBadRequests(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPrivate, true)
	clientID := registerMCPClient(t, f, testRedirect)
	sess := signIn(t, f, "/")

	// A client or redirect URI the server cannot vouch for is the user's
	// to hear about, never a redirect.
	for name, edit := range map[string]func(url.Values){
		"made-up client":      func(q url.Values) { q.Set("client_id", "someone-else") },
		"unregistered target": func(q url.Values) { q.Set("redirect_uri", "https://evil.example/cb") },
	} {
		q := authorizeQuery(clientID)
		edit(q)
		if rec := get(f, "/api/ui/mcp-authorize?"+q.Encode(), sess); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: consent status = %d", name, rec.Code)
		}
		if rec := postJSON(t, f, "/api/ui/mcp-authorize/approve", map[string]string{"query": q.Encode()}, sess); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: approve status = %d", name, rec.Code)
		}
	}
	// Anything else goes back to the client as an OAuth error.
	for name, tc := range map[string]struct {
		edit func(url.Values)
		want string
	}{
		"no PKCE":            {func(q url.Values) { q.Del("code_challenge") }, "invalid_request"},
		"plain PKCE":         {func(q url.Values) { q.Set("code_challenge_method", "plain") }, "invalid_request"},
		"implicit grant":     {func(q url.Values) { q.Set("response_type", "token") }, "unsupported_response_type"},
		"another's resource": {func(q url.Values) { q.Set("resource", "https://elsewhere.example/mcp") }, "invalid_target"},
	} {
		q := authorizeQuery(clientID)
		tc.edit(q)
		dto := decodeJSON[mcpConsentDTO](t, get(f, "/api/ui/mcp-authorize?"+q.Encode(), sess))
		back, _ := url.Parse(dto.Redirect)
		if back == nil || back.Query().Get("error") != tc.want || dto.Client != "" {
			t.Errorf("%s: %+v", name, dto)
		}
	}
	// The approval is a mutation of the session: only from our own pages.
	req := httptest.NewRequest(http.MethodPost, "/api/ui/mcp-authorize/approve", strings.NewReader(`{"query":"`+authorizeQuery(clientID).Encode()+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(sess)
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site approval: status = %d", rec.Code)
	}
}

func TestMCPRegisterValidatesRedirects(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPrivate, true)
	for _, uri := range []string{
		"http://claude.ai/cb", "javascript:alert(1)", "https://claude.ai/cb#frag", "https://user@claude.ai/cb",
		"/relative", "https://claude.ai/" + strings.Repeat("x", maxMCPRedirectURILen),
	} {
		rec := postJSON(t, f, "/mcp/register", map[string]any{"redirect_uris": []string{uri}})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_redirect_uri") {
			t.Errorf("%q: %d %s", uri, rec.Code, rec.Body)
		}
	}
	for _, uri := range []string{"http://localhost:3334/cb", "http://127.0.0.1:9000/callback", "cursor://anysphere.cursor-retrieval/oauth/callback"} {
		registerMCPClient(t, f, uri)
	}
	if rec := postJSON(t, f, "/mcp/register", map[string]any{"redirect_uris": []string{testRedirect}, "grant_types": []string{"client_credentials"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("client_credentials registration: %d", rec.Code)
	}
}

func TestMCPUnknownTokenIsRefused(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	rec := postMCP(f, mcpTokenPrefix+"nope", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	wantStatus(t, rec, "/mcp", http.StatusUnauthorized)
	if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, `error="invalid_token"`) {
		t.Errorf("WWW-Authenticate = %q", got)
	}
}
