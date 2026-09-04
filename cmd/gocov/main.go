// gocov is the coverage uploader CLI, run inside CI:
//
//	gocov upload [flags] coverage.out
//
// Repo, commit, branch and PR id are auto-detected from Bitbucket Pipelines
// or GitHub Actions environment variables, falling back to git. The token
// comes from GOCOV_TOKEN or -token; the server defaults to the hosted
// service (see defaultServer) and only needs -server / $GOCOV_SERVER when
// self-hosting.
package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gocov/gocov/internal/config"
	"github.com/gocov/gocov/internal/hosted"
	"github.com/gocov/gocov/internal/ignore"
	"github.com/gocov/gocov/internal/profile"
)

// version is stamped by the release build via -ldflags "-X main.version=...".
var version = "dev"

// defaultServer is the server used when neither -server nor $GOCOV_SERVER
// is given, so hosted users only supply a token. Self-hosters point at
// their own instance via -server or $GOCOV_SERVER.
var defaultServer = hosted.DefaultServer

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gocov:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "version" {
		fmt.Println("gocov", version)
		return nil
	}
	if len(args) == 0 || args[0] != "upload" {
		return errors.New("usage: gocov upload [flags] <profile file> | gocov version")
	}

	// The environment only supplies flag defaults here; see internal/config.
	cfg, err := config.LoadCLI()
	if err != nil {
		return err
	}
	cfg.Server = cmp.Or(cfg.Server, defaultServer)

	fs := flag.NewFlagSet("upload", flag.ExitOnError)
	server := fs.String("server", cfg.Server, "gocov server URL (or $GOCOV_SERVER)")
	token := fs.String("token", cfg.Token, "per-repo upload token (or $GOCOV_TOKEN)")
	repo := fs.String("repo", "", "repo slug workspace/repo (default: auto-detect)")
	commit := fs.String("commit", "", "commit SHA (default: auto-detect)")
	branch := fs.String("branch", "", "branch name (default: auto-detect)")
	pr := fs.String("pr", "", "pull request id (default: auto-detect)")
	format := fs.String("format", "", "coverage profile format: go, lcov, jacoco, cobertura, clover or simplecov (default: detect from content)")
	pathPrefix := fs.String("path-prefix", "", "prefix mapping profile paths to repo paths, e.g. the Go module path (default: from go.mod)")
	part := fs.String("part", cfg.Part, "name this slice of the commit's coverage (e.g. backend, frontend) when uploading from separate CI jobs (or $GOCOV_PART)")
	ignorePats := patternList{patterns: ignore.Parse(cfg.Ignore)}
	fs.Var(&ignorePats, "ignore", "leave files matching this pattern out of the report, e.g. 'cmd/preview/**' or '*_mock.go'; repeatable or comma-separated (or $GOCOV_IGNORE)")
	failOnGate := fs.Bool("fail-on-gate", false, "exit with a non-zero code when the server reports a failed coverage gate")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: gocov upload [flags] <profile file>")
	}
	profilePath := fs.Arg(0)
	if err := ignore.Validate(ignorePats.patterns); err != nil {
		return err
	}

	if *server == "" {
		return errors.New("server URL required: set -server or $GOCOV_SERVER")
	}
	// Auth precedence when no -token/$GOCOV_TOKEN is given (so existing
	// token users are never affected): mint a forge OIDC identity token if
	// the workflow granted the id-token permission, else fall back to
	// tokenless fork-PR mode (a GitHub Actions pull_request run the server
	// verifies through the App), else error. OIDC removes the pasted secret
	// for a repo's own push and same-repo PR builds; tokenless covers the
	// fork PR that has no secret and no id-token at all.
	// mode names the secret-less auth mode in use — "OIDC" or "tokenless"
	// — and stays empty when a token is sent, which is what decides below
	// whether a refused upload fails the build.
	var mode, oidcToken string
	var ghRun runInfo
	if *token == "" {
		minted, err := mintGitHubOIDC(osEnv, defaultHTTPDoer, *server)
		if err != nil {
			// The id-token permission was present but the request failed.
			// Report it and fall through to the next auth mode; if none
			// applies the upload still errors below, exactly as a missing
			// token would — this line just says why OIDC was not used.
			fmt.Fprintf(os.Stderr, "gocov: OIDC token request failed: %v\n", err)
		}
		// Bitbucket and GitLab hand their OIDC token to the job through
		// the environment directly (no request to make), so it is a read,
		// not a mint.
		oidcToken = cmp.Or(minted, envOIDCToken(osEnv))
		mode = "OIDC"
		if oidcToken == "" {
			ghRun = detectGitHubRun(osEnv, os.ReadFile)
			if !ghRun.tokenlessEligible() {
				return errors.New("no upload credential: set -token or $GOCOV_TOKEN, or enable OIDC " +
					"(GitHub Actions: grant permissions id-token: write; GitLab CI: an id_tokens entry named GOCOV_ID_TOKEN; " +
					"Bitbucket Pipelines: oidc.audiences with the server URL)")
			}
			mode = "tokenless"
		}
	}

	// Flags win over detection: they are the base, and detection only
	// answers what they left open.
	build := buildInfo{Repo: *repo, Commit: *commit, Branch: *branch, PRID: *pr}
	build.fill(detectBuild(osEnv, runGit, os.ReadFile))
	if build.Commit == "" {
		return errors.New("could not detect commit SHA: pass -commit")
	}

	profileData, err := os.ReadFile(profilePath)
	if err != nil {
		return err
	}
	resolvedFormat := cmp.Or(*format, profile.Detect(profileData))
	if resolvedFormat == "" {
		return fmt.Errorf("could not detect the coverage format of %s: pass -format go|lcov|jacoco|cobertura|clover|simplecov", profilePath)
	}
	prefix := *pathPrefix
	if prefix == "" && resolvedFormat == "go" {
		prefix = moduleFromGoMod("go.mod")
	}

	resp, err := upload(uploadRequest{
		Server:       *server,
		Token:        *token,
		OIDCToken:    oidcToken,
		Format:       resolvedFormat,
		PathPrefix:   prefix,
		Part:         *part,
		Ignore:       ignorePats.patterns,
		ProfileData:  profileData,
		ProfileName:  filepath.Base(profilePath),
		Uploader:     "gocov " + version,
		UploaderKind: cfg.Kind(),
		Build:        build,
		Meta:         detectMeta(osEnv, runGit, build.Commit),
		Run:          ghRun,
	})
	if err != nil {
		if mode != "" {
			// A secret-less build must never break over coverage plumbing:
			// one readable line with the server's reason, exit 0.
			verb := "failed"
			if _, ok := errors.AsType[*serverError](err); ok {
				verb = "rejected"
			}
			fmt.Fprintf(os.Stderr, "gocov: %s upload %s — %v\n", mode, verb, err)
			return nil
		}
		return err
	}

	fmt.Printf("uploaded: %.1f%% (%d/%d statements)", resp.TotalPct, resp.CoveredStmts, resp.TotalStmts)
	if resp.DeltaPct != nil {
		fmt.Printf(", delta %+.1f%%", *resp.DeltaPct)
	}
	fmt.Println()
	if resp.RepoCreated {
		fmt.Println("repo registered on first upload")
	}
	switch n := resp.IgnoredFiles; {
	case n == 1:
		fmt.Println("ignored: 1 file")
	case n > 1:
		fmt.Printf("ignored: %d files\n", n)
	}
	if resp.DiffPct != nil && resp.DiffCoveredLines != nil && resp.DiffTotalLines != nil {
		fmt.Printf("diff coverage: %.1f%% (%d/%d changed lines)\n",
			*resp.DiffPct, *resp.DiffCoveredLines, *resp.DiffTotalLines)
	} else if resp.DiffStatus != "" {
		fmt.Printf("diff coverage: %s\n", resp.DiffStatus)
	}
	fmt.Printf("build status: %s\n", resp.BuildStatus)
	if resp.CodeInsights != "" { // empty when talking to an older server
		fmt.Printf("code insights: %s\n", resp.CodeInsights)
	}
	if resp.PRComment != "" {
		fmt.Printf("pr comment: %s\n", resp.PRComment)
	}
	if resp.Gate != "" {
		fmt.Printf("gate: %s\n", resp.Gate)
		if *failOnGate && strings.HasPrefix(resp.Gate, "failed") {
			return fmt.Errorf("coverage gate %s", resp.Gate)
		}
	}
	return nil
}

func osEnv(key string) string { return os.Getenv(key) }

// patternList is the -ignore flag: repeatable, each value one pattern or a
// comma-separated list. It starts out holding $GOCOV_IGNORE; the first
// -ignore on the command line replaces that rather than adding to it, so
// the flag wins over the environment like every other flag.
type patternList struct {
	patterns []string
	fromFlag bool
}

func (l *patternList) String() string { return strings.Join(l.patterns, ",") }

func (l *patternList) Set(v string) error {
	if !l.fromFlag {
		l.patterns, l.fromFlag = nil, true
	}
	l.patterns = append(l.patterns, ignore.Parse(v)...)
	return nil
}
