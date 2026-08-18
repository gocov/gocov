// Package server implements the gocov HTTP API, badge endpoint and web UI.
package server

import (
	"context"
	"embed"
	"fmt"
	"hash/fnv"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/auth"
	"github.com/gocov/gocov/internal/blobstore"
	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/forge/bitbucket"
	"github.com/gocov/gocov/internal/forge/gitlab"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Config wires the server's dependencies. All fields are required except
// Logger, BaseURL and Health.
type Config struct {
	Store   store.Store
	Blobs   blobstore.Store
	Parsers map[string]profile.Parser // by format name, e.g. "go"
	Forges  map[string]forge.Factory  // by forge name, e.g. "bitbucket"
	BaseURL string                    // public URL of this server, for links in build statuses
	Logger  *slog.Logger
	// Health is probed by GET /healthz (e.g. a database ping).
	// When nil, /healthz always reports healthy.
	Health func(ctx context.Context) error
	// DefaultForgeCredentials maps a forge name to fallback credentials
	// (e.g. a global bot account) used for repos that have none of their
	// own. Per-repo credentials always take precedence.
	DefaultForgeCredentials map[string]map[string]string
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
}

// BitbucketConnect runs the Bitbucket OAuth grants for workspace
// connect. Errors wrapping forge.ErrCredentialsRevoked mean the grant
// is gone (revoked, or the refresh token aged out unused).
type BitbucketConnect interface {
	// AuthorizeURL is the consent page for the connect grant.
	AuthorizeURL(state, redirectURI string) string
	// Exchange trades the consent code for the grant, including the
	// granting account's username.
	Exchange(ctx context.Context, code, redirectURI string) (*bitbucket.Grant, error)
	// Refresh trades a refresh token for a fresh access token and — the
	// tokens rotate — a new refresh token to persist.
	Refresh(ctx context.Context, refreshToken string) (*bitbucket.Grant, error)
	// ForgeClient returns a forge client acting through the access token.
	ForgeClient(accessToken string) forge.Forge
}

// GitLabConnect runs the GitLab OAuth grants for workspace connect —
// BitbucketConnect's twin. Errors wrapping forge.ErrCredentialsRevoked
// mean the grant is gone (revoked on the account's applications page).
type GitLabConnect interface {
	// AuthorizeURL is the consent page for the connect grant (scope api).
	AuthorizeURL(state, redirectURI string) string
	// Exchange trades the consent code for the grant, including the
	// granting account's username.
	Exchange(ctx context.Context, code, redirectURI string) (*gitlab.Grant, error)
	// Refresh trades a refresh token for a fresh access token and — the
	// tokens rotate — a new refresh token to persist. GitLab's token
	// endpoint wants the redirect URI on refreshes too.
	Refresh(ctx context.Context, refreshToken, redirectURI string) (*gitlab.Grant, error)
	// ForgeClient returns a forge client acting through the access token.
	ForgeClient(accessToken string) forge.Forge
}

// GitHubApp mints installation-scoped forge clients and answers the two
// questions the connect flow needs. Errors wrapping
// forge.ErrCredentialsRevoked mean the installation (or the app's own
// credentials) no longer exists on GitHub.
type GitHubApp interface {
	// ForgeClient returns a forge client authenticated as the given
	// installation.
	ForgeClient(ctx context.Context, installationID int64) (forge.Forge, error)
	// InstallationAccount returns the login of the org or user account
	// the installation lives on.
	InstallationAccount(ctx context.Context, installationID int64) (string, error)
	// InstallURL is the app's public install page on GitHub.
	InstallURL(ctx context.Context) (string, error)
}

// Server is the gocov HTTP server.
type Server struct {
	store         store.Store
	blobs         blobstore.Store
	parsers       map[string]profile.Parser
	forges        map[string]forge.Factory
	baseURL       string
	log           *slog.Logger
	pages         map[string]*template.Template
	mux           *http.ServeMux
	handler       http.Handler // mux wrapped in the auth middleware
	health        func(ctx context.Context) error
	defaultCreds  map[string]map[string]string
	githubApp     GitHubApp
	bbConnect     BitbucketConnect
	bbTokens      *bbTokenCache
	glConnect     GitLabConnect
	glTokens      *bbTokenCache // same cache/locking shape, separate tokens
	webhookSecret string

	// auths holds the sign-in providers by forge name; authOrder keeps
	// the configured order for the login-page buttons.
	auths             map[string]auth.Provider
	authOrder         []auth.Provider
	allowedWorkspaces []string
	hosted            bool
	// secureCookies marks auth cookies Secure when the public base URL is
	// https (the UI is then served through TLS or a terminating proxy).
	secureCookies bool
}

// New builds a Server; panics only on programmer error (bad templates).
func New(cfg Config) *Server {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	assetVer := staticVersion()
	funcs := template.FuncMap{
		"pct": func(v float64) string { return fmt.Sprintf("%.1f%%", v) },
		"short": func(sha string) string {
			if len(sha) > 12 {
				return sha[:12]
			}
			return sha
		},
		"ranges":   diffcov.Ranges,
		"covclass": covClass,
		"timeago":  timeAgo,
		// asset appends a content-derived version so browsers refetch
		// embedded assets after a server upgrade despite long cache TTLs.
		"asset": func(path string) string { return path + "?v=" + assetVer },
		// pathesc encodes a value into a single URL path segment; GitLab
		// workspace prefixes contain slashes and must ride as %2F.
		"pathesc": url.PathEscape,
		// forgeicon renders a forge's brand mark for the sign-in button.
		"forgeicon": providerIcon,
	}
	// Every page is its own template set sharing the layout and partials,
	// so pages can define "content" without colliding.
	pages := map[string]*template.Template{}
	for _, name := range []string{"index.html", "repo.html", "upload.html", "source.html", "login.html",
		"register.html", "workspace.html", "setup.html", "connect.html"} {
		pages[name] = template.Must(template.New(name).Funcs(funcs).ParseFS(templatesFS,
			"templates/layout.html", "templates/partials.html", "templates/"+name))
	}

	s := &Server{
		store:         cfg.Store,
		blobs:         cfg.Blobs,
		parsers:       cfg.Parsers,
		forges:        cfg.Forges,
		baseURL:       cfg.BaseURL,
		log:           log,
		pages:         pages,
		mux:           http.NewServeMux(),
		health:        cfg.Health,
		defaultCreds:  cfg.DefaultForgeCredentials,
		githubApp:     cfg.GitHubApp,
		bbConnect:     cfg.BitbucketConnect,
		bbTokens:      newBBTokenCache(),
		glConnect:     cfg.GitLabConnect,
		glTokens:      newBBTokenCache(),
		webhookSecret: cfg.GitHubWebhookSecret,

		auths:             map[string]auth.Provider{},
		authOrder:         cfg.Auths,
		allowedWorkspaces: cfg.AllowedWorkspaces,
		hosted:            cfg.Hosted,
		secureCookies:     strings.HasPrefix(cfg.BaseURL, "https://"),
	}
	for _, p := range cfg.Auths {
		s.auths[p.Name()] = p
	}
	s.routes()
	s.handler = s.requireAuth(s.mux)
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/v1/upload", s.handleUpload)
	s.mux.HandleFunc("GET /badge/{slug...}", s.handleBadge)
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.Handle("GET /static/", cacheStatic(http.FileServerFS(staticFS)))
	s.mux.HandleFunc("GET /login", s.handleLogin)
	s.mux.HandleFunc("GET /oauth/{forge}/start", s.handleOAuthStart)
	s.mux.HandleFunc("GET /oauth/{forge}/callback", s.handleOAuthCallback)
	s.mux.HandleFunc("POST /logout", s.handleLogout)
	s.mux.HandleFunc("GET /register", s.handleRegisterPage)
	s.mux.HandleFunc("POST /register", s.handleRegister)
	s.mux.HandleFunc("GET /workspaces/{prefix}", s.handleWorkspacePage)
	s.mux.HandleFunc("POST /workspaces/{prefix}/rotate-token", s.handleWorkspaceRotate)
	s.mux.HandleFunc("POST /workspaces/{prefix}/settings", s.handleWorkspaceSettings)
	s.mux.HandleFunc("POST /workspaces/{prefix}/credentials", s.handleWorkspaceCredentials)
	s.mux.HandleFunc("POST /workspaces/{prefix}/github/disconnect", s.handleGitHubDisconnect)
	s.mux.HandleFunc("GET /workspaces/{prefix}/bitbucket/connect", s.handleBitbucketConnect)
	s.mux.HandleFunc("POST /workspaces/{prefix}/bitbucket/disconnect", s.handleBitbucketDisconnect)
	s.mux.HandleFunc("GET /workspaces/{prefix}/gitlab/connect", s.handleGitLabConnect)
	s.mux.HandleFunc("POST /workspaces/{prefix}/gitlab/disconnect", s.handleGitLabDisconnect)
	s.mux.HandleFunc("GET /workspaces/{prefix}/setup", s.handleWorkspaceSetup)
	s.mux.HandleFunc("GET /workspaces/{prefix}/setup/status", s.handleWorkspaceSetupStatus)
	s.mux.HandleFunc("GET /github/setup", s.handleGitHubSetup)
	if s.webhookSecret != "" {
		s.mux.HandleFunc("POST /github/webhook", s.handleGitHubWebhook)
	}
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /repos/{slug...}", s.handleRepo)
	s.mux.HandleFunc("GET /uploads/{id}", s.handleUploadPage)
	s.mux.HandleFunc("GET /uploads/{id}/files/{path...}", s.handleSource)
}

// cacheStatic adds cache headers for the embedded assets. URLs carry a
// content-derived ?v=, so long-lived caching is safe across upgrades.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}

// staticVersion hashes the embedded static files, changing whenever their
// content changes.
func staticVersion() string {
	h := fnv.New64a()
	_ = fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := staticFS.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = h.Write([]byte(path))
		_, _ = h.Write(data)
		return nil
	})
	return fmt.Sprintf("%x", h.Sum64())
}

// handleHealthz reports readiness: 200 when the health probe (typically a
// database ping) succeeds, 503 otherwise.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if s.health != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
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

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	t, ok := s.pages[name]
	if !ok {
		s.log.Error("unknown page template", "template", name)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Layout-level auth state: the open-UI banner and the nav user chip.
	data["AuthOpen"] = !s.authEnabled()
	data["CurrentUser"] = currentUser(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		s.log.Error("render template", "template", name, "err", err)
	}
}

// renderPartial executes one named block of a page's template set without
// the layout — the response bodies of htmx poll endpoints.
func (s *Server) renderPartial(w http.ResponseWriter, page, block string, data any) {
	t, ok := s.pages[page]
	if !ok {
		s.log.Error("unknown page template", "template", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, block, data); err != nil {
		s.log.Error("render partial", "template", page, "block", block, "err", err)
	}
}

// covClass maps a percentage to the badge threshold classes.
func covClass(p float64) string {
	switch {
	case p < 50:
		return "bad"
	case p <= 75:
		return "warn"
	default:
		return "good"
	}
}

// timeAgo renders a compact relative timestamp for tables.
func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 14*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("2006-01-02")
	}
}
