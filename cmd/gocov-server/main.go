// gocov-server runs the gocov API + web UI:
//
//	gocov-server serve                # default when no subcommand given
//	gocov-server version
//
// Workspaces, repos, gates and members are all administered from the web
// UI (sign in, then use the dashboard and workspace settings); hosted mode
// adds self-service registration at /register.
//
// Configuration is environment-only, and the list of variables is not
// repeated here: it is the tagged Server struct in internal/config, which
// is where a new setting is added, with docs/configuration.md as the same
// list written out for self-hosters and a test holding the two in step.
// In outline — DATABASE_URL is required; each GOCOV_OAUTH_*_KEY and
// _SECRET pair turns on web UI sign-in for that forge; GOCOV_SECRET_KEY
// on top of a pair enables Bitbucket or GitLab workspace connect, while
// the GitHub equivalent rides GOCOV_GITHUB_APP_ID and its private key;
// GOCOV_MODE=hosted opens self-service registration to any forge account.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gocov/gocov/internal/auth"
	authbb "github.com/gocov/gocov/internal/auth/bitbucket"
	authgh "github.com/gocov/gocov/internal/auth/github"
	authgl "github.com/gocov/gocov/internal/auth/gitlab"
	blobpg "github.com/gocov/gocov/internal/blobstore/postgres"
	"github.com/gocov/gocov/internal/config"
	"github.com/gocov/gocov/internal/forge/bitbucket"
	"github.com/gocov/gocov/internal/forge/github"
	"github.com/gocov/gocov/internal/forge/gitlab"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/secretbox"
	"github.com/gocov/gocov/internal/server"
	storepg "github.com/gocov/gocov/internal/store/postgres"
)

// version is stamped by the release build via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gocov-server:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return serve()
	}
	switch args[0] {
	case "version":
		fmt.Println("gocov-server", version)
		return nil
	case "serve":
		return serve()
	default:
		return fmt.Errorf("unknown command %q (want serve|version)", args[0])
	}
}

func connect(ctx context.Context, cfg config.Server) (*storepg.Store, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}
	st := storepg.New(pool)
	// The at-rest cipher for stored grant tokens (One-Click Connect D6).
	if cfg.SecretKey != "" {
		box, err := secretbox.New(cfg.SecretKey)
		if err != nil {
			return nil, fmt.Errorf("GOCOV_SECRET_KEY: %w", err)
		}
		st.SetCipher(box)
	}
	if err := st.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("applying migrations: %w", err)
	}
	return st, nil
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// The whole environment contract is read and validated here, once,
	// before anything is wired up; see internal/config. Warnings are
	// logged ahead of the error check on purpose: a fatal misconfiguration
	// should not hide a second, survivable one that the operator would
	// otherwise only meet on the next restart.
	cfg, err := config.LoadServer()
	for _, warning := range cfg.Warnings() {
		log.Warn(warning)
	}
	if err != nil {
		return err
	}

	st, err := connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Pool().Close()

	app, err := githubApp(cfg, log)
	if err != nil {
		return err
	}

	callback := func(forge string) string {
		return strings.TrimSuffix(cfg.BaseURL, "/") + "/oauth/" + forge + "/callback"
	}
	var authProviders []auth.Provider
	if cfg.Bitbucket.Configured() {
		authProviders = append(authProviders, authbb.New(cfg.Bitbucket.Key, cfg.Bitbucket.Secret))
		log.Info("bitbucket sign-in enabled", "callback", callback("bitbucket"))
	}
	if cfg.GitHub.Configured() {
		authProviders = append(authProviders, authgh.New(cfg.GitHub.Key, cfg.GitHub.Secret))
		log.Info("github sign-in enabled", "callback", callback("github"))
	}
	if cfg.GitLab.Configured() {
		authProviders = append(authProviders, authgl.New(cfg.GitLab.Key, cfg.GitLab.Secret))
		log.Info("gitlab sign-in enabled", "callback", callback("gitlab"))
	}
	if len(authProviders) == 0 {
		log.Info("no sign-in provider configured; web UI stays open")
	}
	if cfg.Hosted() {
		log.Info("hosted mode: self-service workspace registration enabled")
	}

	srvCfg := server.Config{
		Store: st,
		Blobs: blobpg.New(st.Pool()),
		Parsers: map[string]profile.Parser{
			"go":        profile.GoParser{},
			"lcov":      profile.LCOVParser{},
			"jacoco":    profile.JaCoCoParser{},
			"cobertura": profile.CoberturaParser{},
			"clover":    profile.CloverParser{},
			"simplecov": profile.SimpleCovParser{},
		},
		BaseURL: cfg.BaseURL,
		Logger:  log,
		Health:  st.Pool().Ping,

		Auths:             authProviders,
		AllowedWorkspaces: cfg.AllowedWorkspaces,
		Hosted:            cfg.Hosted(),
	}
	// Assigned conditionally: a typed-nil *github.App in the interface
	// field would read as "configured".
	if app != nil {
		srvCfg.GitHubApp = app
	}
	if cfg.GitHubWebhookSecret != "" {
		srvCfg.GitHubWebhookSecret = cfg.GitHubWebhookSecret
		log.Info("github webhook endpoint enabled")
	}
	switch {
	case cfg.BitbucketConnectEnabled():
		srvCfg.BitbucketConnect = &bitbucket.Consumer{Key: cfg.Bitbucket.Key, Secret: cfg.Bitbucket.Secret}
		log.Info("bitbucket workspace connect enabled")
	case cfg.Bitbucket.Configured():
		log.Info("GOCOV_SECRET_KEY not set; Bitbucket workspace connect stays disabled")
	}
	switch {
	case cfg.GitLabConnectEnabled():
		srvCfg.GitLabConnect = &gitlab.Application{Key: cfg.GitLab.Key, Secret: cfg.GitLab.Secret}
		log.Info("gitlab workspace connect enabled")
	case cfg.GitLab.Configured():
		log.Info("GOCOV_SECRET_KEY not set; GitLab workspace connect stays disabled")
	}
	srv := server.New(srvCfg)

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.Addr, "base_url", cfg.BaseURL)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		// SIGINT/SIGTERM: finish in-flight requests, then exit cleanly.
		// Releasing the signal handler first lets a second signal kill the
		// process the default way if shutdown hangs.
		stop()
		log.Info("shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

// githubApp builds the deployment's GitHub App identity from the
// configured id and private key. The key holds either the PEM itself or a
// path to a PEM file — key files are how GitHub hands the secret out, but
// container setups often prefer the content in the environment. A
// half-configured pair is already reported by config.Server.Warnings, so
// it just leaves the integration off here.
func githubApp(cfg config.Server, log *slog.Logger) (*github.App, error) {
	if !cfg.GitHubAppEnabled() {
		return nil, nil
	}
	pemData := []byte(cfg.GitHubAppPrivateKey)
	if !strings.Contains(cfg.GitHubAppPrivateKey, "-----BEGIN") {
		data, err := os.ReadFile(cfg.GitHubAppPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("reading GOCOV_GITHUB_APP_PRIVATE_KEY file: %w", err)
		}
		pemData = data
	}
	app, err := github.NewApp(cfg.GitHubAppID, pemData)
	if err != nil {
		return nil, fmt.Errorf("GOCOV_GITHUB_APP_PRIVATE_KEY: %w", err)
	}
	log.Info("github app configured", "app_id", cfg.GitHubAppID)
	return app, nil
}
