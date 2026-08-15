// gocov-server runs the gocov API + web UI, and provides repo administration:
//
//	gocov-server serve                # default when no subcommand given
//	gocov-server repo add -slug workspace/repo [flags]
//	gocov-server repo list
//	gocov-server repo rotate-token -slug workspace/repo
//	gocov-server repo update -slug workspace/repo [flags]
//	gocov-server repo remove -slug workspace/repo -force
//	gocov-server workspace add -prefix workspace [flags]
//	gocov-server workspace list|rotate-token|update|remove
//	gocov-server user list
//	gocov-server user remove -email someone@example.com
//
// Configuration via environment: DATABASE_URL (required), GOCOV_ADDR
// (default :8080), GOCOV_BASE_URL (default http://localhost:8080), and
// optionally GOCOV_BITBUCKET_USERNAME / GOCOV_BITBUCKET_APP_PASSWORD
// and/or GOCOV_GITHUB_TOKEN for global bot credentials used by repos
// without their own.
// Setting GOCOV_OAUTH_BITBUCKET_KEY / GOCOV_OAUTH_BITBUCKET_SECRET (a
// Bitbucket OAuth consumer) and/or GOCOV_OAUTH_GITHUB_KEY /
// GOCOV_OAUTH_GITHUB_SECRET (a GitHub OAuth app) enables — and from then
// on requires — sign-in for the web UI, one login button per configured
// forge; GOCOV_ALLOWED_WORKSPACES (comma-separated) optionally overrides
// which workspace/org members may sign in.
// GOCOV_MODE=hosted switches to self-service mode: any forge account may
// sign in and register its own workspaces from the UI. The default
// (private) keeps sign-in limited to members of tracked workspaces.
// GOCOV_GITHUB_APP_ID and GOCOV_GITHUB_APP_PRIVATE_KEY (PEM content, or
// a path to a PEM file) enable the GitHub App integration: workspaces
// connect with one click and statuses, PR comments and check runs are
// posted as the app's bot identity, no tokens required.
// GOCOV_SECRET_KEY (exactly 64 hex characters, e.g. `openssl rand -hex
// 32`; the AES key is a plain SHA-256 of it, so the value must carry a
// full 256 bits of entropy and boot fails on anything else) enables the
// Bitbucket workspace connect on top of the Bitbucket OAuth consumer:
// one consent and the workspace acts through that grant, its refresh
// token stored encrypted.
// GOCOV_GITHUB_WEBHOOK_SECRET enables the GitHub App / Marketplace
// webhook at POST /github/webhook, verifying delivery signatures against
// this secret (required for a Marketplace listing).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gocov/gocov/internal/auth"
	authbb "github.com/gocov/gocov/internal/auth/bitbucket"
	authgh "github.com/gocov/gocov/internal/auth/github"
	blobpg "github.com/gocov/gocov/internal/blobstore/postgres"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/forge/bitbucket"
	"github.com/gocov/gocov/internal/forge/github"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/secretbox"
	"github.com/gocov/gocov/internal/server"
	storepg "github.com/gocov/gocov/internal/store/postgres"
)

// version is stamped by the release build via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		if !errors.Is(err, errPrinted) {
			fmt.Fprintln(os.Stderr, "gocov-server:", err)
		}
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
	case "repo":
		ctx := context.Background()
		st, err := connect(ctx)
		if err != nil {
			return err
		}
		defer st.Pool().Close()
		return repoCmd(ctx, st, blobpg.New(st.Pool()), args[1:], os.Stdout)
	case "workspace":
		ctx := context.Background()
		st, err := connect(ctx)
		if err != nil {
			return err
		}
		defer st.Pool().Close()
		return workspaceCmd(ctx, st, args[1:], os.Stdout)
	case "user":
		ctx := context.Background()
		st, err := connect(ctx)
		if err != nil {
			return err
		}
		defer st.Pool().Close()
		return userCmd(ctx, st, args[1:], os.Stdout)
	default:
		return fmt.Errorf("unknown command %q (want serve|repo|workspace|user)", args[0])
	}
}

// secretKeyPattern is the required shape of GOCOV_SECRET_KEY: exactly 64
// hex characters (256 bits), either case.
var secretKeyPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// secretKey validates GOCOV_SECRET_KEY and returns the usable key, or ""
// when unset. The at-rest AES key is a plain SHA-256 of this value
// (secretbox.New), with no salt or work factor, so the value itself must
// carry the full 256 bits of entropy — a guessable passphrase would be
// brute-forceable offline against a leaked database. We therefore refuse
// anything that isn't 64 hex characters at boot rather than sealing
// tokens under a weak key. Derivation is left untouched, so an existing
// 64-hex key keeps producing the same cipher. An unset or blank value is
// not an error: it just leaves Bitbucket workspace connect disabled.
func secretKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", nil
	}
	if !secretKeyPattern.MatchString(key) {
		return "", errors.New("GOCOV_SECRET_KEY must be 64 hex characters.\nGenerate one with: openssl rand -hex 32")
	}
	return key, nil
}

func connect(ctx context.Context) (*storepg.Store, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}
	st := storepg.New(pool)
	// The at-rest cipher for Bitbucket grant tokens (One-Click Connect
	// D6); set here so the CLI subcommands share it with serve.
	key, err := secretKey(os.Getenv("GOCOV_SECRET_KEY"))
	if err != nil {
		return nil, err
	}
	if key != "" {
		box, err := secretbox.New(key)
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

	st, err := connect(ctx)
	if err != nil {
		return err
	}
	defer st.Pool().Close()

	addr := envOr("GOCOV_ADDR", ":8080")
	baseURL := envOr("GOCOV_BASE_URL", "http://localhost:8080")

	defaultCreds := map[string]map[string]string{}
	bbUser, bbPassword := os.Getenv("GOCOV_BITBUCKET_USERNAME"), os.Getenv("GOCOV_BITBUCKET_APP_PASSWORD")
	switch {
	case bbUser != "" && bbPassword != "":
		defaultCreds["bitbucket"] = map[string]string{"username": bbUser, "app_password": bbPassword}
		log.Info("global bitbucket credentials configured", "username", bbUser)
	case bbUser != "" || bbPassword != "":
		log.Warn("GOCOV_BITBUCKET_USERNAME and GOCOV_BITBUCKET_APP_PASSWORD must both be set; ignoring")
	}
	if ghToken := os.Getenv("GOCOV_GITHUB_TOKEN"); ghToken != "" {
		defaultCreds["github"] = map[string]string{"token": ghToken}
		log.Info("global github credentials configured")
	}

	githubApp, err := githubAppFromEnv(log)
	if err != nil {
		return err
	}

	// Configuring an OAuth consumer/app is the switch that turns sign-in
	// on; without one the UI stays open and shows a banner saying so.
	var authProviders []auth.Provider
	oauthKey, oauthSecret := os.Getenv("GOCOV_OAUTH_BITBUCKET_KEY"), os.Getenv("GOCOV_OAUTH_BITBUCKET_SECRET")
	switch {
	case oauthKey != "" && oauthSecret != "":
		authProviders = append(authProviders, authbb.New(oauthKey, oauthSecret))
		log.Info("bitbucket sign-in enabled", "callback", strings.TrimSuffix(baseURL, "/")+"/oauth/bitbucket/callback")
	case oauthKey != "" || oauthSecret != "":
		log.Warn("GOCOV_OAUTH_BITBUCKET_KEY and GOCOV_OAUTH_BITBUCKET_SECRET must both be set; ignoring")
	}
	ghKey, ghSecret := os.Getenv("GOCOV_OAUTH_GITHUB_KEY"), os.Getenv("GOCOV_OAUTH_GITHUB_SECRET")
	switch {
	case ghKey != "" && ghSecret != "":
		authProviders = append(authProviders, authgh.New(ghKey, ghSecret))
		log.Info("github sign-in enabled", "callback", strings.TrimSuffix(baseURL, "/")+"/oauth/github/callback")
	case ghKey != "" || ghSecret != "":
		log.Warn("GOCOV_OAUTH_GITHUB_KEY and GOCOV_OAUTH_GITHUB_SECRET must both be set; ignoring")
	}
	if len(authProviders) == 0 {
		log.Info("no sign-in provider configured; web UI stays open")
	}
	var hosted bool
	switch mode := envOr("GOCOV_MODE", "private"); mode {
	case "private":
	case "hosted":
		// Self-service registration is meaningless without a forge
		// identity to derive claimable workspaces from; fail fast.
		if len(authProviders) == 0 {
			return fmt.Errorf("GOCOV_MODE=hosted requires a sign-in provider (set GOCOV_OAUTH_*_KEY/SECRET)")
		}
		hosted = true
		log.Info("hosted mode: self-service workspace registration enabled")
	default:
		return fmt.Errorf("GOCOV_MODE=%q: want private or hosted", mode)
	}
	var allowedWorkspaces []string
	for _, ws := range strings.Split(os.Getenv("GOCOV_ALLOWED_WORKSPACES"), ",") {
		if ws = strings.TrimSpace(ws); ws != "" {
			allowedWorkspaces = append(allowedWorkspaces, ws)
		}
	}

	cfg := server.Config{
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
		Forges: map[string]forge.Factory{
			"bitbucket": bitbucket.Factory,
			"github":    github.Factory,
		},
		BaseURL: baseURL,
		Logger:  log,
		Health:  st.Pool().Ping,

		DefaultForgeCredentials: defaultCreds,
		Auths:                   authProviders,
		AllowedWorkspaces:       allowedWorkspaces,
		Hosted:                  hosted,
	}
	// Assigned conditionally: a typed-nil *github.App in the interface
	// field would read as "configured".
	if githubApp != nil {
		cfg.GitHubApp = githubApp
	}
	if secret := os.Getenv("GOCOV_GITHUB_WEBHOOK_SECRET"); secret != "" {
		cfg.GitHubWebhookSecret = secret
		log.Info("github webhook endpoint enabled")
	}
	// Bitbucket workspace connect (One-Click Connect P2) rides the
	// sign-in consumer, and needs the at-rest cipher for the stored
	// refresh token.
	switch {
	case oauthKey != "" && oauthSecret != "" && strings.TrimSpace(os.Getenv("GOCOV_SECRET_KEY")) != "":
		cfg.BitbucketConnect = &bitbucket.Consumer{Key: oauthKey, Secret: oauthSecret}
		log.Info("bitbucket workspace connect enabled")
	case oauthKey != "" && oauthSecret != "":
		log.Info("GOCOV_SECRET_KEY not set; Bitbucket workspace connect stays disabled")
	}
	srv := server.New(cfg)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	log.Info("listening", "addr", addr, "base_url", baseURL)

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

// githubAppFromEnv builds the deployment's GitHub App identity from
// GOCOV_GITHUB_APP_ID and GOCOV_GITHUB_APP_PRIVATE_KEY. The key variable
// holds either the PEM itself or a path to a PEM file — key files are how
// GitHub hands the secret out, but container setups often prefer the
// content in the environment.
func githubAppFromEnv(log *slog.Logger) (*github.App, error) {
	appID, appKey := os.Getenv("GOCOV_GITHUB_APP_ID"), os.Getenv("GOCOV_GITHUB_APP_PRIVATE_KEY")
	switch {
	case appID == "" && appKey == "":
		return nil, nil
	case appID == "" || appKey == "":
		log.Warn("GOCOV_GITHUB_APP_ID and GOCOV_GITHUB_APP_PRIVATE_KEY must both be set; ignoring")
		return nil, nil
	}
	pemData := []byte(appKey)
	if !strings.Contains(appKey, "-----BEGIN") {
		data, err := os.ReadFile(appKey)
		if err != nil {
			return nil, fmt.Errorf("reading GOCOV_GITHUB_APP_PRIVATE_KEY file: %w", err)
		}
		pemData = data
	}
	app, err := github.NewApp(appID, pemData)
	if err != nil {
		return nil, fmt.Errorf("GOCOV_GITHUB_APP_PRIVATE_KEY: %w", err)
	}
	log.Info("github app configured", "app_id", appID)
	return app, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
