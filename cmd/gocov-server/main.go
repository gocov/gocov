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
//
// This file is the process: parse the command line, boot, serve, shut
// down. What the environment turns into is next door in wiring.go.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	blobpg "github.com/gocov/gocov/internal/blobstore/postgres"
	"github.com/gocov/gocov/internal/config"
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

	srvCfg, err := buildServerConfig(cfg, deps{
		Store:  st,
		Blobs:  blobpg.New(st.Pool()),
		Health: st.Pool().Ping,
	}, log)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.New(srvCfg),
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
