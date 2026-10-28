// Signing an MCP client in: the OAuth 2.1 authorization server behind /mcp
// (mcp.go), so a connector given only the URL — a claude.ai custom
// connector, say — reads the reports its user may see, private ones
// included, without a token pasted anywhere.
//
// It is the flow the MCP authorization spec asks for, and no more of OAuth
// than that: /mcp names its authorization server in its protected-resource
// metadata (RFC 9728), the server describes itself (RFC 8414), a client
// registers itself dynamically (RFC 7591), and the user approves it on a
// consent page of the app, signed in the way they always are. The code that
// approval issues is exchanged, under PKCE, for an access token.
//
// The token reads reports and nothing else: it is a row of its own, never a
// session, so the most a leaked one can do is what get_report does. There
// is no refresh token; when one expires the client signs in again.
//
// Registration keeps nothing. A client's id is its own registration —
// its name and redirect URIs, encoded — which is all an open registration
// endpoint could vouch for anyway. What stands between a made-up client
// and a user's reports is the consent page, which names where the approval
// is sent.

package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/store"
)

const (
	// mcpTokenTTL is how long a connector reads before signing in again:
	// a web session's lifetime.
	mcpTokenTTL = sessionTTL
	// mcpCodeTTL bounds the hop from the consent page to the token
	// endpoint, which a client makes at once.
	mcpCodeTTL = 5 * time.Minute
	// mcpTokenPrefix marks gocov's MCP access tokens, so one pasted where
	// it should not be is recognizable as what it is.
	mcpTokenPrefix = "gocov_mcp_"
	// mcpClientPrefix marks a client id this server issued.
	mcpClientPrefix = "mcp_"

	// What a client may register, kept small enough that its id still
	// fits in the sign-in round trip's state cookie.
	maxMCPRedirectURIs   = 4
	maxMCPRedirectURILen = 300
	maxMCPClientNameLen  = 80
)

// mcpAuthEnabled reports whether /mcp signs its clients in: only behind
// sign-in, and only with a public URL to name itself by. An open instance
// serves every repo to anyone, so there is nothing to sign in to.
func (s *Server) mcpAuthEnabled() bool { return s.authEnabled() && s.baseURL != "" }

// mcpResource is /mcp's own URL, the resource a token is issued for.
func (s *Server) mcpResource() string { return s.baseURL + "/mcp" }

// mcpResourceMetadataURL is where /mcp's protected-resource metadata
// lives: the well-known path with the resource's path appended (RFC 9728).
func (s *Server) mcpResourceMetadataURL() string {
	return s.baseURL + "/.well-known/oauth-protected-resource/mcp"
}

func (s *Server) mcpAuthRoutes() {
	if !s.mcpAuthEnabled() {
		return
	}
	s.mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.handleMCPResourceMetadata)
	s.mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.handleMCPResourceMetadata)
	s.mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleMCPServerMetadata)
	s.mux.HandleFunc("POST /mcp/register", s.handleMCPRegister)
	s.mux.HandleFunc("POST /mcp/token", s.handleMCPToken)
	s.mux.HandleFunc("GET /mcp/authorize", s.handleMCPAuthorizePage)
}

// handleMCPAuthorizePage serves the consent page: the app's, signed in
// like any other page, but never inside another site's frame, where an
// approval could be clicked by someone who cannot see what it approves.
func (s *Server) handleMCPAuthorizePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	s.handleAppPage(w, r)
}

// handleMCP serves /mcp. On an instance that signs its clients in, every
// message carries a token: without one the answer is the 401 that sends an
// MCP client to sign in, naming where to start. The session cookie is
// never consulted, so no page can make a browser call it.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if !s.mcpAuthEnabled() {
		s.mcp.ServeHTTP(w, r)
		return
	}
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		s.mcpUnauthorized(w, "")
		return
	}
	u, err := s.store.UserByMCPToken(r.Context(), hashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		s.mcpUnauthorized(w, "invalid_token")
		return
	}
	if err != nil {
		s.internalError(w, "mcp token lookup", err)
		return
	}
	s.mcp.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
}

// mcpUnauthorized answers a message that needs a token (RFC 6750 §3).
func (s *Server) mcpUnauthorized(w http.ResponseWriter, oauthErr string) {
	challenge := `Bearer resource_metadata="` + s.mcpResourceMetadataURL() + `"`
	if oauthErr != "" {
		challenge += `, error="` + oauthErr + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	httpError(w, http.StatusUnauthorized, "sign-in required")
}

// handleMCPResourceMetadata describes /mcp as a protected resource: which
// authorization server signs its clients in.
func (s *Server) handleMCPResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	s.writeOAuthJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.mcpResource(),
		"resource_name":            "gocov",
		"authorization_servers":    []string{s.baseURL},
		"bearer_methods_supported": []string{"header"},
	})
}

// handleMCPServerMetadata describes the authorization server: the flow is
// the authorization code under PKCE, for public clients only.
func (s *Server) handleMCPServerMetadata(w http.ResponseWriter, _ *http.Request) {
	s.writeOAuthJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         s.baseURL,
		"authorization_endpoint":                         s.baseURL + "/mcp/authorize",
		"token_endpoint":                                 s.baseURL + "/mcp/token",
		"registration_endpoint":                          s.baseURL + "/mcp/register",
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"authorization_response_iss_parameter_supported": true,
	})
}

// mcpClient is a registration, and its encoding is the client's id.
type mcpClient struct {
	Name         string   `json:"n,omitempty"`
	RedirectURIs []string `json:"r"`
}

func (c mcpClient) id() string {
	b, _ := json.Marshal(c)
	return mcpClientPrefix + base64.RawURLEncoding.EncodeToString(b)
}

// displayName is what the consent page calls the client.
func (c mcpClient) displayName() string {
	if c.Name != "" {
		return c.Name
	}
	return "An MCP client"
}

// parseMCPClient reads a client id back into its registration, refusing
// anything this server could not have issued.
func parseMCPClient(id string) (mcpClient, bool) {
	enc, ok := strings.CutPrefix(id, mcpClientPrefix)
	if !ok || len(enc) > 4096 {
		return mcpClient{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return mcpClient{}, false
	}
	var c mcpClient
	if json.Unmarshal(b, &c) != nil || validateMCPClient(c) != "" {
		return mcpClient{}, false
	}
	return c, true
}

// validateMCPClient checks a registration, returning what is wrong with it
// or "" when nothing is.
func validateMCPClient(c mcpClient) string {
	if len(c.RedirectURIs) == 0 || len(c.RedirectURIs) > maxMCPRedirectURIs {
		return "register between 1 and 4 redirect_uris"
	}
	for _, u := range c.RedirectURIs {
		if !validMCPRedirectURI(u) {
			return "unsupported redirect_uri " + u
		}
	}
	if len(c.Name) > maxMCPClientNameLen || !utf8.ValidString(c.Name) {
		return "client_name is too long"
	}
	return ""
}

// validMCPRedirectURI admits where an approval may be sent: an https URL,
// http only to this machine (a desktop client's loopback listener), or a
// desktop app's own scheme (RFC 8252). Never a scheme a browser would run
// rather than navigate to, and never a fragment, which a code may not ride.
func validMCPRedirectURI(raw string) bool {
	if raw == "" || len(raw) > maxMCPRedirectURILen {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.User != nil || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.Host != ""
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	case "javascript", "data", "file", "vbscript", "blob", "about", "ftp", "ws", "wss":
		return false
	}
	return true
}

// redirectOrigin is how the consent page names where an approval goes: the
// scheme and host, which is what tells one destination from another.
func redirectOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Host == "" {
		return u.Scheme + ":"
	}
	return u.Scheme + "://" + u.Host
}

// handleMCPRegister implements dynamic client registration (RFC 7591):
// public clients only, so the answer carries no secret, only the id.
func (s *Server) handleMCPRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RedirectURIs []string `json:"redirect_uris"`
		ClientName   string   `json:"client_name"`
		GrantTypes   []string `json:"grant_types"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAPIBody)).Decode(&req); err != nil {
		s.oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "malformed registration")
		return
	}
	if len(req.GrantTypes) > 0 && !slices.Contains(req.GrantTypes, "authorization_code") {
		s.oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "only the authorization_code grant is supported")
		return
	}
	c := mcpClient{Name: strings.TrimSpace(req.ClientName), RedirectURIs: req.RedirectURIs}
	if len(c.Name) > maxMCPClientNameLen {
		c.Name = strings.ToValidUTF8(c.Name[:maxMCPClientNameLen], "")
	}
	if msg := validateMCPClient(c); msg != "" {
		s.oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", msg)
		return
	}
	resp := map[string]any{
		"client_id":                  c.id(),
		"client_id_issued_at":        time.Now().Unix(),
		"redirect_uris":              c.RedirectURIs,
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}
	if c.Name != "" {
		resp["client_name"] = c.Name
	}
	s.writeOAuthJSON(w, http.StatusCreated, resp)
}

// mcpAuthRequest is an authorization request, checked.
type mcpAuthRequest struct {
	client      mcpClient
	clientID    string
	redirectURI string
	state       string
	challenge   string
}

// parseMCPAuthRequest checks an authorization request's query. A client or
// redirect URI it cannot vouch for is a bad link, answered to the user
// (bad); anything else wrong goes back to the client as an OAuth error
// (errRedirect), since by then the redirect URI is known to be the
// client's own.
func (s *Server) parseMCPAuthRequest(q url.Values) (req *mcpAuthRequest, errRedirect string, bad bool) {
	c, ok := parseMCPClient(q.Get("client_id"))
	if !ok {
		return nil, "", true
	}
	redirectURI := q.Get("redirect_uri")
	if redirectURI == "" && len(c.RedirectURIs) == 1 {
		redirectURI = c.RedirectURIs[0]
	}
	if !slices.Contains(c.RedirectURIs, redirectURI) {
		return nil, "", true
	}
	req = &mcpAuthRequest{client: c, clientID: q.Get("client_id"), redirectURI: redirectURI, state: q.Get("state"), challenge: q.Get("code_challenge")}
	fail := func(code, desc string) (*mcpAuthRequest, string, bool) {
		return nil, s.authRedirect(req, url.Values{"error": {code}, "error_description": {desc}}), false
	}
	switch {
	case q.Get("response_type") != "code":
		return fail("unsupported_response_type", "only response_type=code is supported")
	case req.challenge == "" || q.Get("code_challenge_method") != "S256":
		return fail("invalid_request", "PKCE with code_challenge_method=S256 is required")
	case !s.mcpResourceAccepted(q.Get("resource")):
		return fail("invalid_target", "this server issues tokens for "+s.mcpResource()+" only")
	}
	return req, "", false
}

// mcpResourceAccepted reports whether a request's resource indicator (RFC
// 8707) names /mcp, or is left out.
func (s *Server) mcpResourceAccepted(resource string) bool {
	resource = strings.TrimSuffix(resource, "/")
	return resource == "" || resource == s.mcpResource() || resource == s.baseURL
}

// authRedirect is the client's redirect URI carrying an authorization
// response, with the state echoed and the issuer named (RFC 9207).
func (s *Server) authRedirect(req *mcpAuthRequest, params url.Values) string {
	u, err := url.Parse(req.redirectURI)
	if err != nil {
		return req.redirectURI
	}
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	if req.state != "" {
		q.Set("state", req.state)
	}
	q.Set("iss", s.baseURL)
	u.RawQuery = q.Encode()
	return u.String()
}

// mcpConsentDTO is what the consent page shows, or where it sends the
// browser straight away when the request is one to refuse.
type mcpConsentDTO struct {
	// Redirect, when set, is the only field: the request goes back to
	// its client with an error.
	Redirect string `json:"redirect,omitempty"`
	Client   string `json:"client,omitempty"`
	// RedirectOrigin is where an approval is sent.
	RedirectOrigin string `json:"redirect_origin,omitempty"`
	// DenyURL is where declining sends the browser.
	DenyURL string `json:"deny_url,omitempty"`
	User    string `json:"user,omitempty"`
}

// handleAPIMCPConsent implements GET /api/ui/mcp-authorize: the consent
// page's reading of the authorization request in its own query.
func (s *Server) handleAPIMCPConsent(w http.ResponseWriter, r *http.Request) {
	if !s.mcpAuthEnabled() {
		httpError(w, http.StatusNotFound, "not found")
		return
	}
	req, errRedirect, bad := s.parseMCPAuthRequest(r.URL.Query())
	switch {
	case bad:
		invalid(w, "This sign-in link is not one gocov issued, or it was changed on the way.")
		return
	case errRedirect != "":
		s.writeJSON(w, mcpConsentDTO{Redirect: errRedirect})
		return
	}
	u := currentUser(r)
	s.writeJSON(w, mcpConsentDTO{
		Client:         req.client.displayName(),
		RedirectOrigin: redirectOrigin(req.redirectURI),
		DenyURL:        s.authRedirect(req, url.Values{"error": {"access_denied"}}),
		User:           u.DisplayName,
	})
}

// handleAPIMCPApprove implements POST /api/ui/mcp-authorize/approve: the
// user's approval of the request in the posted query, answered with the
// client's redirect URI carrying a fresh code.
func (s *Server) handleAPIMCPApprove(w http.ResponseWriter, r *http.Request) {
	if !s.mcpAuthEnabled() {
		httpError(w, http.StatusNotFound, "not found")
		return
	}
	var body struct {
		Query string `json:"query"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	q, err := url.ParseQuery(strings.TrimPrefix(body.Query, "?"))
	if err != nil {
		invalid(w, "This sign-in link is not one gocov issued, or it was changed on the way.")
		return
	}
	req, errRedirect, bad := s.parseMCPAuthRequest(q)
	switch {
	case bad:
		invalid(w, "This sign-in link is not one gocov issued, or it was changed on the way.")
		return
	case errRedirect != "":
		s.writeJSON(w, mcpConsentDTO{Redirect: errRedirect})
		return
	}
	code := core.RandomHex(32)
	if err := s.store.CreateOAuthCode(r.Context(), &store.OAuthCode{
		CodeHash:      hashToken(code),
		UserID:        currentUser(r).ID,
		ClientID:      req.clientID,
		RedirectURI:   req.redirectURI,
		CodeChallenge: req.challenge,
		ExpiresAt:     time.Now().Add(mcpCodeTTL),
	}); err != nil {
		s.internalError(w, "creating authorization code", err)
		return
	}
	s.log.Info("mcp client approved", "user", currentUser(r).DisplayName, "client", req.client.displayName(), "redirect", redirectOrigin(req.redirectURI))
	s.writeJSON(w, mcpConsentDTO{Redirect: s.authRedirect(req, url.Values{"code": {code}})})
}

// handleMCPToken implements the token endpoint: an authorization code,
// with the PKCE verifier its challenge was made from, for an access token.
func (s *Server) handleMCPToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAPIBody)
	if err := r.ParseForm(); err != nil {
		s.oauthError(w, http.StatusBadRequest, "invalid_request", "malformed request")
		return
	}
	if gt := r.PostForm.Get("grant_type"); gt != "authorization_code" {
		s.oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "only the authorization_code grant is supported")
		return
	}
	code, verifier := r.PostForm.Get("code"), r.PostForm.Get("code_verifier")
	if code == "" || verifier == "" {
		s.oauthError(w, http.StatusBadRequest, "invalid_request", "code and code_verifier are required")
		return
	}
	c, err := s.store.ConsumeOAuthCode(r.Context(), hashToken(code))
	if errors.Is(err, store.ErrNotFound) {
		s.oauthError(w, http.StatusBadRequest, "invalid_grant", "the code is unknown, used or expired")
		return
	}
	if err != nil {
		s.internalError(w, "consuming authorization code", err)
		return
	}
	// The code is spent either way: a mismatch below may be an attacker
	// holding a stolen one, and the client starts over.
	clientID := r.PostForm.Get("client_id")
	redirectURI := r.PostForm.Get("redirect_uri")
	if clientID == "" {
		clientID = c.ClientID
	}
	if clientID != c.ClientID || (redirectURI != "" && redirectURI != c.RedirectURI) || !pkceMatches(verifier, c.CodeChallenge) {
		s.oauthError(w, http.StatusBadRequest, "invalid_grant", "the code was issued to another client, redirect URI or verifier")
		return
	}
	if !s.mcpResourceAccepted(r.PostForm.Get("resource")) {
		s.oauthError(w, http.StatusBadRequest, "invalid_target", "this server issues tokens for "+s.mcpResource()+" only")
		return
	}
	client, _ := parseMCPClient(c.ClientID)
	token := mcpTokenPrefix + core.RandomHex(32)
	if err := s.store.CreateMCPToken(r.Context(), &store.MCPToken{
		TokenHash:  hashToken(token),
		UserID:     c.UserID,
		ClientName: client.Name,
		ExpiresAt:  time.Now().Add(mcpTokenTTL),
	}); err != nil {
		s.internalError(w, "creating mcp token", err)
		return
	}
	s.writeOAuthJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(mcpTokenTTL.Seconds()),
	})
}

// pkceMatches checks a PKCE verifier against its S256 challenge (RFC 7636).
func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(want), []byte(challenge)) == 1
}

// writeOAuthJSON answers an OAuth endpoint: JSON no cache may keep.
func (s *Server) writeOAuthJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// oauthError answers in OAuth's error shape (RFC 6749 §5.2).
func (s *Server) oauthError(w http.ResponseWriter, code int, oauthErr, desc string) {
	s.writeOAuthJSON(w, code, map[string]string{"error": oauthErr, "error_description": desc})
}
