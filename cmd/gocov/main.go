// gocov is the coverage uploader CLI, run inside CI:
//
//	gocov upload [flags] coverage.out
//
// and reads a commit's report back, for a person or an agent:
//
//	gocov report [flags]
//
// or serves that report to an MCP client over stdio:
//
//	gocov mcp [flags]
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
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gocov/gocov/internal/config"
	"github.com/gocov/gocov/internal/hosted"
	"github.com/gocov/gocov/internal/ignore"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/readapi"
)

// version is stamped by the release build via -ldflags "-X main.version=...".
var version = "dev"

// defaultServer is the server used when neither -server nor $GOCOV_SERVER
// is given, so hosted users only supply a token. Self-hosters point at
// their own instance via -server or $GOCOV_SERVER.
var defaultServer = hosted.DefaultServer

// exitGateFailed is the exit status of an upload that landed and whose
// commit the server then judged to fail its coverage gate, under
// -fail-on-gate. It is not 1 because that is every other error, and a CI
// wrapper has to tell the two apart: a failed gate is a verdict on the
// change, an upload that broke is coverage plumbing, and a pipeline may
// want to tolerate the second without waving the first through. It is not
// 2 because the flag package exits with that on a bad upload flag, as the
// Go runtime does on a panic. docs/cli.md gives the number to pipelines to
// branch on, so it does not change.
const exitGateFailed = 3

// gateFailedError is what run returns for the exitGateFailed case. verdict
// is the server's own: "failed: <reasons>".
type gateFailedError struct{ verdict string }

func (e *gateFailedError) Error() string { return "coverage gate " + e.verdict }

func main() {
	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "gocov:", err)
	}
	os.Exit(exitStatus(err))
}

// exitStatus is the process's exit status for what run returned: 0 for no
// error, exitGateFailed for a failed gate, 1 for everything else.
func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	if _, ok := errors.AsType[*gateFailedError](err); ok {
		return exitGateFailed
	}
	return 1
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "version" {
		fmt.Println("gocov", version)
		return nil
	}
	if len(args) > 0 && args[0] == "report" {
		return runReport(args[1:], os.Stdout, osEnv, runGit, defaultHTTPDoer)
	}
	if len(args) > 0 && args[0] == "mcp" {
		return runMCP(args[1:], os.Stdin, os.Stdout, osEnv, runGit, defaultHTTPDoer)
	}
	if len(args) == 0 || args[0] != "upload" {
		return errors.New("usage: gocov upload [flags] <profile file> | gocov report [flags] | gocov mcp [flags] | gocov version")
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
	format := fs.String("format", "", "coverage profile format: "+strings.Join(profile.Names(), ", ")+" (default: detect from content)")
	pathPrefix := fs.String("path-prefix", "", "prefix mapping profile paths to repo paths, e.g. the Go module path (default: from go.mod for Go, the checkout directory for OpenCover and xccov)")
	part := fs.String("part", cfg.Part, "name this slice of the commit's coverage (e.g. backend, frontend) when uploading from separate CI jobs (or $GOCOV_PART)")
	ignorePats := patternList{patterns: ignore.Parse(cfg.Ignore)}
	fs.Var(&ignorePats, "ignore", "leave files matching this pattern out of the report, e.g. 'cmd/preview/**' or '*_mock.go'; repeatable or comma-separated (or $GOCOV_IGNORE)")
	failOnGate := fs.Bool("fail-on-gate", false, fmt.Sprintf("exit with status %d when the server reports a failed coverage gate (an upload that fails exits 1)", exitGateFailed))
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
	// The Actions event payload feeds both the tokenless run identity and
	// build detection; read it once.
	ghEvent := readGitHubEvent(osEnv, os.ReadFile)
	var ghRun runInfo
	if *token == "" {
		minted, mintErr := mintGitHubOIDC(osEnv, defaultHTTPDoer, *server)
		if mintErr != nil {
			// The id-token permission was present but the request failed.
			// Report it and fall through to the next auth mode; if none
			// applies the upload errors below and says why.
			fmt.Fprintf(os.Stderr, "gocov: OIDC token request failed: %v\n", mintErr)
		}
		// Bitbucket and GitLab hand their OIDC token to the job through
		// the environment directly (no request to make), so it is a read,
		// not a mint.
		oidcToken = cmp.Or(minted, envOIDCToken(osEnv))
		mode = "OIDC"
		if oidcToken == "" {
			ghRun = detectGitHubRun(osEnv, ghEvent)
			if !ghRun.tokenlessEligible() {
				// The permission is there and the request failed: telling
				// the user to grant it would send them the wrong way.
				if mintErr != nil {
					return fmt.Errorf("no upload credential: the id-token permission is set but GitHub's OIDC token request failed (%w); re-run the job, or set -token or $GOCOV_TOKEN", mintErr)
				}
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
	build.fill(detectBuild(osEnv, runGit, ghEvent))
	if build.Commit == "" {
		return errors.New("could not detect commit SHA: pass -commit")
	}

	profileData, err := os.ReadFile(profilePath)
	if err != nil {
		return err
	}
	resolvedFormat := *format
	if resolvedFormat == "" {
		resolvedFormat = profile.Detect(profileData)
	}
	if resolvedFormat == "" {
		return fmt.Errorf("could not detect the coverage format of %s: pass -format %s", profilePath, strings.Join(profile.Names(), "|"))
	}
	prefix := *pathPrefix
	if prefix == "" {
		if resolvedFormat == "go" {
			prefix = moduleFromGoMod("go.mod")
		} else if f, ok := profile.Lookup(resolvedFormat); ok && f.AbsolutePaths {
			prefix = reportPrefix(f, profileData, runGit)
		}
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

	printResult(os.Stdout, resp)
	if *failOnGate && strings.HasPrefix(resp.Gate, "failed") {
		return &gateFailedError{verdict: resp.Gate}
	}
	return nil
}

// printResult writes the upload's outcome to the CI log, one line per
// fact the server reported.
func printResult(w io.Writer, resp *uploadResponse) {
	// The totals are the commit's merged report, not this upload's. With
	// one default part the two are the same; with named parts, say which
	// parts the figure covers so a total read before the last part lands
	// isn't mistaken for the commit's.
	parted := resp.Part != "" && !(resp.Part == "default" && len(resp.Parts) <= 1)
	if parted {
		fmt.Fprintf(w, "uploaded part: %s\n", resp.Part)
		fmt.Fprintf(w, "commit coverage: %.1f%% (%d/%d statements)", resp.TotalPct, resp.CoveredStmts, resp.TotalStmts)
	} else {
		fmt.Fprintf(w, "uploaded: %.1f%% (%d/%d statements)", resp.TotalPct, resp.CoveredStmts, resp.TotalStmts)
	}
	if resp.DeltaPct != nil {
		fmt.Fprintf(w, ", delta %+.1f%%", *resp.DeltaPct)
	}
	fmt.Fprintln(w)
	if parted && len(resp.Parts) > 0 {
		noun := "parts"
		if len(resp.Parts) == 1 {
			noun = "part"
		}
		fmt.Fprintf(w, "merged from %d %s so far: %s\n", len(resp.Parts), noun, strings.Join(resp.Parts, ", "))
	}
	// A carried part is an earlier commit's measurement standing in for a
	// job this commit did not run; name where each came from.
	if len(resp.Carried) > 0 {
		from := make([]string, len(resp.Carried))
		for i, c := range resp.Carried {
			from[i] = c.Part + " from " + readapi.Short(c.Commit)
		}
		fmt.Fprintf(w, "carried forward: %s\n", strings.Join(from, ", "))
	}
	for _, warning := range resp.Warnings {
		fmt.Fprintf(w, "warning: %s\n", warning)
	}
	if resp.RepoCreated {
		fmt.Fprintln(w, "repo registered on first upload")
	}
	switch n := resp.IgnoredFiles; {
	case n == 1:
		fmt.Fprintln(w, "ignored: 1 file")
	case n > 1:
		fmt.Fprintf(w, "ignored: %d files\n", n)
	}
	if resp.DiffPct != nil && resp.DiffCoveredLines != nil && resp.DiffTotalLines != nil {
		fmt.Fprintf(w, "diff coverage: %.1f%% (%d/%d changed lines)\n",
			*resp.DiffPct, *resp.DiffCoveredLines, *resp.DiffTotalLines)
	} else if resp.DiffStatus != "" {
		fmt.Fprintf(w, "diff coverage: %s\n", resp.DiffStatus)
	}
	fmt.Fprintf(w, "build status: %s\n", resp.BuildStatus)
	if resp.CodeInsights != "" { // empty when talking to an older server
		fmt.Fprintf(w, "code insights: %s\n", resp.CodeInsights)
	}
	if resp.PRComment != "" {
		fmt.Fprintf(w, "pr comment: %s\n", resp.PRComment)
	}
	if resp.Gate != "" {
		fmt.Fprintf(w, "gate: %s\n", resp.Gate)
	}
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
