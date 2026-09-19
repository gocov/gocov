// Package server implements the gocov HTTP API, badge endpoint and web UI.
// The UI itself is the single-page app in web/: every page route answers
// with its shell (spa.go) and the app reads its data from /api/ui/
// (api.go). Only the favicon is left of the server's own assets.
package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/auth"
	"github.com/gocov/gocov/internal/blobstore"
	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/oidc"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

//go:embed static
var staticFS embed.FS

// Config wires the server's dependencies. All fields are required except
// Logger, BaseURL and Health.
type Config struct {
	Store   store.Store
	Blobs   blobstore.Store
	Parsers map[string]profile.Parser // by format name, e.g. "go"
	BaseURL string                    // public URL of this server, for links in build statuses
	Logger  *slog.Logger
	// Health is probed by GET /healthz (e.g. a database ping).
	// When nil, /healthz always reports healthy.
	Health func(ctx context.Context) error
	// Auths enables web UI sign-in, one provider per forge, rendered as
	// one login button each. Empty keeps the UI open (with a banner
	// explaining how to enable sign-in); the upload API, badges and health
	// checks are unaffected either way.
	Auths []auth.Provider
	// AllowedWorkspaces overrides the derived "tracked workspaces" set
	// that gates who may sign in. Empty means derive from the store.
	AllowedWorkspaces []string
	// Hosted switches the instance to self-service mode (M3/D1): any
	// forge account may sign in, and users without a tracked-workspace
	// membership are routed to the registration page instead of being
	// denied. False keeps today's private behavior exactly.
	Hosted bool
	// GitHubApp is the deployment's GitHub App identity (One-Click
	// Connect P1), implemented by forge/github.App. Nil when the
	// deployment has none; the credential chain then starts at repo
	// credentials exactly as before.
	GitHubApp GitHubApp
	// BitbucketConnect is the OAuth consumer powering the Bitbucket
	// workspace-connect grant (One-Click Connect P2), implemented by
	// forge/bitbucket.Consumer. Nil disables the feature; requires
	// GOCOV_SECRET_KEY at the store for the at-rest token encryption.
	BitbucketConnect BitbucketConnect
	// GitLabConnect is the OAuth application powering the GitLab
	// workspace-connect grant, implemented by forge/gitlab.Application.
	// Nil disables the feature; requires GOCOV_SECRET_KEY at the store
	// for the at-rest token encryption, and the application must carry
	// the "api" scope on top of sign-in's read scopes.
	GitLabConnect GitLabConnect
	// GitHubWebhookSecret enables the GitHub App / Marketplace webhook
	// (POST /github/webhook) and is the HMAC secret its signatures are
	// verified against. Empty leaves the route unregistered.
	GitHubWebhookSecret string
	// PublicReports allows anonymous read-only report pages for repos the
	// forge reports public (GOCOV_PUBLIC_REPORTS). False keeps every page
	// behind the login wall exactly as before.
	PublicReports bool
	// OIDCVerifier verifies the forge-minted OIDC identity tokens that let a
	// repo's own CI upload without a pasted token (server/oidc.go). Nil
	// builds the default: the public forge issuers, with this server's
	// BaseURL as the required audience. Tests inject one pointed at a local
	// issuer.
	OIDCVerifier *oidc.Verifier
	// OIDCIssuers lists extra trusted OIDC issuers beyond the public forge
	// ones (GOCOV_OIDC_ISSUERS): self-managed GitLab instance URLs whose CI
	// ID tokens name repos by project_path, the same as gitlab.com.
	OIDCIssuers []string
	// PostHog turns on the browser analytics snippet in the web UI
	// (analytics.go). The zero value leaves every page free of third-party
	// scripts, which is what self-hosted deployments get by default.
	PostHog PostHog
}

// The forge connectors a deployment can configure. They are declared in
// internal/core, which owns the connections and their upkeep; the aliases
// keep this package's Config the one place a caller has to look.
type (
	GitHubApp        = core.GitHubApp
	BitbucketConnect = core.BitbucketConnect
	GitLabConnect    = core.GitLabConnect
)

// Server is the gocov HTTP server.
type Server struct {
	store         store.Store
	blobs         blobstore.Store
	parsers       map[string]profile.Parser
	baseURL       string
	log           *slog.Logger
	mux           *http.ServeMux
	handler       http.Handler // mux wrapped in the auth middleware
	health        func(ctx context.Context) error
	forges        *core.Forges
	webhookSecret string
	// pipeline is the coverage logic proper: gate, merge, forge report.
	pipeline *core.Pipeline
	// tokenless rate-limits tokenless upload attempts per repo.
	tokenless *tokenlessLimiter
	// oidc verifies forge-minted OIDC identity tokens for tokenless uploads
	// from a repo's own CI (server/oidc.go).
	oidc *oidc.Verifier
	// gitlabIssuers is the set of trusted GitLab OIDC issuers — gitlab.com
	// plus any operator-configured self-managed instances — used to route a
	// verified token to the gitlab claim mapping.
	gitlabIssuers map[string]bool

	// auths holds the sign-in providers by forge name; authOrder keeps
	// the configured order for the login-page buttons.
	auths             map[string]auth.Provider
	authOrder         []auth.Provider
	allowedWorkspaces []string
	hosted            bool
	publicReports     bool
	// secureCookies marks auth cookies Secure when the public base URL is
	// https (the UI is then served through TLS or a terminating proxy).
	secureCookies bool
	// posthog is the analytics snippet configuration; zero means off.
	posthog PostHog
}

// New builds a Server.
func New(cfg Config) *Server {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	// The trusted GitLab issuers. A gocov deployment connects to exactly one
	// GitLab, so it must trust exactly that instance's issuer — not gitlab.com
	// *and* a self-managed one at once, which would let a token from either
	// authenticate an upload to a same-named project on the other (GitLab
	// resolves by project path, and paths are not unique across instances).
	// So the operator's configured issuers replace the gitlab.com default
	// rather than adding to it: unset means gitlab.com; set means exactly the
	// listed self-managed instances. GitHub and Bitbucket are unaffected.
	// Both the token router (oidcForge) and the verifier's issuer allowlist
	// draw from this set.
	gitlabIssuers := map[string]bool{}
	for _, iss := range cfg.OIDCIssuers {
		if iss = strings.TrimRight(iss, "/"); iss != "" {
			gitlabIssuers[iss] = true
		}
	}
	if len(gitlabIssuers) == 0 {
		gitlabIssuers[gitLabDotComIssuer] = true
	}

	oidcVerifier := cfg.OIDCVerifier
	if oidcVerifier == nil && cfg.BaseURL != "" {
		// A token's aud must equal this server's public URL, so a token
		// minted for another instance cannot be replayed here. Without a
		// BaseURL there is no audience to bind to, so OIDC uploads stay off.
		exactIssuers := []string{gitHubActionsIssuer}
		for iss := range gitlabIssuers {
			exactIssuers = append(exactIssuers, iss)
		}
		oidcVerifier = oidc.New(oidc.Config{
			Audience:      cfg.BaseURL,
			Issuers:       exactIssuers,
			ResolveIssuer: bitbucketIssuerResolver(cfg.Store),
		})
	}

	s := &Server{
		store:         cfg.Store,
		blobs:         cfg.Blobs,
		parsers:       cfg.Parsers,
		baseURL:       cfg.BaseURL,
		log:           log,
		mux:           http.NewServeMux(),
		health:        cfg.Health,
		forges:        core.NewForges(cfg.Store, log, cfg.BaseURL, cfg.GitHubApp, cfg.BitbucketConnect, cfg.GitLabConnect),
		webhookSecret: cfg.GitHubWebhookSecret,
		tokenless:     newTokenlessLimiter(),
		oidc:          oidcVerifier,
		gitlabIssuers: gitlabIssuers,

		auths:             map[string]auth.Provider{},
		authOrder:         cfg.Auths,
		allowedWorkspaces: cfg.AllowedWorkspaces,
		hosted:            cfg.Hosted,
		publicReports:     cfg.PublicReports,
		secureCookies:     strings.HasPrefix(cfg.BaseURL, "https://"),
		posthog:           cfg.PostHog,
	}
	// Everything that decides rather than transports lives in core; the
	// server holds one handle to it.
	s.pipeline = &core.Pipeline{Store: cfg.Store, Blobs: cfg.Blobs, Log: log, BaseURL: cfg.BaseURL, Forges: s.forges, Hosted: cfg.Hosted}
	for _, p := range cfg.Auths {
		s.auths[p.Name()] = p
	}
	s.routes()
	s.handler = s.requireAuth(s.mux)
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/v1/upload", s.handleUpload)
	s.mux.HandleFunc("GET /badge/{forge}/{slug...}", s.handleBadge)
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /robots.txt", s.handleRobots)
	s.mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
	s.mux.Handle("GET /static/", cacheStatic(http.FileServerFS(staticFS)))
	s.mux.HandleFunc("GET /login", s.handleLogin)
	s.mux.HandleFunc("GET /oauth/{forge}/start", s.handleOAuthStart)
	s.mux.HandleFunc("GET /oauth/{forge}/callback", s.handleOAuthCallback)
	s.mux.HandleFunc("POST /logout", s.handleLogout)
	s.mux.HandleFunc("GET /github/setup", s.handleGitHubSetup)
	if s.webhookSecret != "" {
		s.mux.HandleFunc("POST /github/webhook", s.handleGitHubWebhook)
	}
	// The workspace's forge decides what connect means: the GitHub App
	// link, or the Bitbucket/GitLab consent grant. It is a browser
	// navigation into the forge's consent screen, so it stays a route of
	// its own rather than moving to the UI API.
	s.mux.HandleFunc("GET /workspace-connect/{forge}/{prefix...}", s.handleConnect)
	s.mux.HandleFunc("GET /uploads/{id}/profile", s.handleUploadProfile)
	s.spaRoutes()
	s.apiRoutes()

	// The page routes. Each serves the app shell (spa.go) — the status
	// code and the head tags are Go's, the body is the app's. Tenant
	// pages carry the forge before the name: repo slugs and workspace
	// prefixes are unique per forge, not globally (urls.go). Repo slugs
	// contain a slash (workspace/repo), and GitLab workspace prefixes can
	// (grp/sub), so both ride as a trailing {slug...} / {prefix...}
	// wildcard — a single segment cannot match one on a live server (only
	// httptest preserves the %2F).
	s.mux.HandleFunc("GET /{$}", s.handleHome)
	s.mux.HandleFunc("GET /onboarding", s.handleAppPage)
	s.mux.HandleFunc("GET /_components", s.handleAppPage)
	s.mux.HandleFunc("GET /w/{forge}/{prefix...}", s.handleAppPage)
	s.mux.HandleFunc("GET /workspace-settings/{forge}/{prefix...}", s.handleAppPage)
	s.mux.HandleFunc("GET /workspace-setup/{forge}/{prefix...}", s.handleAppPage)
	// Where those three lived through v0.25 (urls.go).
	s.mux.HandleFunc("GET /workspaces/{forge}/{prefix}", s.handleLegacyWorkspacePage)
	s.mux.HandleFunc("GET /workspaces/{forge}/{prefix}/setup", s.handleLegacyWorkspacePage)
	s.mux.HandleFunc("GET /repo-settings/{forge}/{slug...}", s.handleAppPage)
	// The report pages settle their own access before serving the shell,
	// so a hidden repo answers exactly as it did (D3).
	s.mux.HandleFunc("GET /repos/{forge}/{slug...}", s.handleRepo)
	s.mux.HandleFunc("GET /uploads/{id}", s.handleUploadPage)
	s.mux.HandleFunc("GET /uploads/{id}/files/{path...}", s.handleSource)
	// Least-specific pattern: anything no route above claims lands here. A
	// browser GET gets the shell with a 404; other methods/clients keep the
	// plain-text 404 (so an unmatched path 404s rather than 405s).
	s.mux.HandleFunc("/", s.handleNotFound)
}

// cacheStatic adds cache headers for the server's own embedded assets,
// which is now the favicon alone. The app's bundles live under
// /static/app/ and are cached by their own, more specific route (spa.go).
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}

// HealthTimeout is how long GET /healthz gives its readiness probe before
// calling the instance unhealthy. It is exported because whoever polls the
// endpoint has to outwait it to get an answer rather than a timeout of
// their own — see healthProbeTimeout in cmd/gocov-server.
const HealthTimeout = 2 * time.Second

// handleHealthz reports readiness: 200 when the health probe (typically a
// database ping) succeeds, 503 otherwise.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if s.health != nil {
		ctx, cancel := context.WithTimeout(r.Context(), HealthTimeout)
		defer cancel()
		if err := s.health(ctx); err != nil {
			s.log.Error("health check", "err", err)
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// handleNotFound is the catch-all for paths no route claims. Browser
// navigations (a GET that accepts HTML) get the app shell with a 404, so
// the app draws its not-found panel; everything else — API clients, other
// methods — keeps the plain-text 404.
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	// A signed-out request can only land here through the public-report
	// pass-through (an unrouted path under /repos/ or /uploads/); answer
	// with the login redirect every other signed-out request gets, so the
	// signed-out response surface stays uniform.
	if s.authEnabled() && currentUser(r) == nil {
		redirectToLogin(w, r)
		return
	}
	if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
		s.serveApp(w, r, http.StatusNotFound, appHead{})
		return
	}
	http.NotFound(w, r)
}

// shortSHA abbreviates a commit identifier for display.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func httpError(w http.ResponseWriter, code int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf(format, args...)})
}

func (s *Server) internalError(w http.ResponseWriter, msg string, err error) {
	s.log.Error(msg, "err", err)
	httpError(w, http.StatusInternalServerError, "internal error")
}
