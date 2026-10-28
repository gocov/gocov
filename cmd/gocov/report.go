package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gocov/gocov/internal/config"
)

// gocov report reads a commit's merged report back from the server's read
// API (GET /api/v1/reports/…) and prints what an agent or a person fixing
// the change needs: the totals, and the changed lines no test ran. A
// public repo needs no credential; a private one is read with whatever its
// upload would authenticate with — the token, or in CI the forge's OIDC
// token — found the same way an upload finds it.

// reportResponse is the read API's answer, as much of it as the CLI prints.
// -json passes the server's body through untouched instead.
type reportResponse struct {
	Repo struct {
		Forge string `json:"forge"`
		Slug  string `json:"slug"`
	} `json:"repo"`
	Commit     string   `json:"commit"`
	Branch     string   `json:"branch"`
	PRID       string   `json:"pr_id"`
	Parts      int      `json:"parts"`
	Coverage   float64  `json:"coverage"`
	GateFailed bool     `json:"gate_failed"`
	Delta      *float64 `json:"delta"`
	Base       *struct {
		Commit string `json:"commit"`
	} `json:"base"`
	Diff *struct {
		Coverage     float64 `json:"coverage"`
		CoveredLines int64   `json:"covered_lines"`
		TotalLines   int64   `json:"total_lines"`
		Uncovered    []struct {
			Path  string `json:"path"`
			Lines string `json:"lines"`
		} `json:"uncovered"`
		UnmatchedFiles []string `json:"unmatched_files"`
	} `json:"diff"`
	ReportURL string `json:"report_url"`
}

func runReport(args []string, stdout io.Writer, env envFunc, git gitFunc, doer httpDoer) error {
	cfg, err := config.LoadCLI()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	q := reportQuery{}
	fs.StringVar(&q.server, "server", cmp.Or(cfg.Server, defaultServer), "gocov server URL (or $GOCOV_SERVER)")
	fs.StringVar(&q.forge, "forge", "", "github, gitlab or bitbucket (default: auto-detect)")
	fs.StringVar(&q.repo, "repo", "", "repo slug workspace/repo (default: auto-detect)")
	fs.StringVar(&q.commit, "commit", "", "commit SHA (default: auto-detect; ignored with -branch)")
	fs.StringVar(&q.branch, "branch", "", "report the newest commit on this branch instead of one commit")
	fs.StringVar(&q.token, "token", cfg.Token, "upload token, for a private repo (or $GOCOV_TOKEN)")
	asJSON := fs.Bool("json", false, "print the server's JSON answer as is")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: gocov report [flags]")
	}
	body, err := fetchReport(q, env, git, doer)
	if err != nil {
		return err
	}
	if *asJSON {
		_, err := stdout.Write(body)
		return err
	}
	var rep reportResponse
	if err := json.Unmarshal(body, &rep); err != nil {
		return fmt.Errorf("decoding report: %w", err)
	}
	printReport(stdout, &rep)
	return nil
}

// reportQuery names the report to read: empty fields are detected, and an
// empty token is looked for the way an upload looks for one.
type reportQuery struct {
	server, forge, repo, commit, branch, token string
}

// fetchReport reads one report from the read API and returns its JSON
// body, or an error that says why there is none.
func fetchReport(q reportQuery, env envFunc, git gitFunc, doer httpDoer) ([]byte, error) {
	build := buildInfo{Repo: q.repo, Commit: q.commit}
	if q.repo == "" || (q.commit == "" && q.branch == "") {
		build.fill(detectBuild(env, git, readGitHubEvent(env, os.ReadFile)))
	}
	if build.Repo == "" {
		return nil, errors.New("repo not detected: set -repo workspace/repo")
	}
	f := cmp.Or(q.forge, detectForge(env, git))
	if f == "" {
		return nil, errors.New("forge not detected: set -forge github, gitlab or bitbucket")
	}

	v := url.Values{}
	if q.branch != "" {
		v.Set("branch", q.branch)
	} else if build.Commit != "" {
		v.Set("commit", build.Commit)
	} else {
		return nil, errors.New("commit not detected: set -commit, or -branch for a branch's newest report")
	}
	segs := strings.Split(build.Repo, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	u := fmt.Sprintf("%s/api/v1/reports/%s/%s?%s", strings.TrimRight(q.server, "/"),
		url.PathEscape(f), strings.Join(segs, "/"), v.Encode())
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gocov/"+version)
	// The upload's credential order: a token, else an OIDC token the CI
	// mints or hands over. None at all reads public reports only.
	cred, mintErr := q.token, error(nil)
	if cred == "" {
		var minted string
		minted, mintErr = mintGitHubOIDC(env, doer, q.server)
		cred = cmp.Or(minted, envOIDCToken(env))
	}
	if cred != "" {
		req.Header.Set("Authorization", "Bearer "+cred)
	}
	status, body, err := send(doer, req)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		why := "its reports are not public: set -token or $GOCOV_TOKEN to read a private repo"
		switch {
		case cred != "":
			why = "the credential is not for this repo and its reports are not public"
		case mintErr != nil:
			why = fmt.Sprintf("its reports are not public, and GitHub's OIDC token request failed (%v)", mintErr)
		}
		return nil, fmt.Errorf("no report for %s/%s (%s): it is not uploaded yet, or %s", f, build.Repo, v.Encode(), why)
	}
	if status != http.StatusOK {
		return nil, &serverError{code: status, msg: strings.TrimSpace(string(body))}
	}
	return body, nil
}

// printReport writes the report as a few plain lines: easy to read, and
// easy for an agent to act on line by line.
func printReport(w io.Writer, r *reportResponse) {
	head := fmt.Sprintf("%s/%s @ %s", r.Repo.Forge, r.Repo.Slug, short(r.Commit))
	if r.PRID != "" {
		head += fmt.Sprintf(" (%s, PR %s)", r.Branch, r.PRID)
	} else if r.Branch != "" {
		head += fmt.Sprintf(" (%s)", r.Branch)
	}
	fmt.Fprintln(w, head)

	cov := fmt.Sprintf("coverage: %.1f%%", r.Coverage)
	if r.Delta != nil && r.Base != nil {
		cov += fmt.Sprintf(" (%+.1f%% vs %s)", *r.Delta, short(r.Base.Commit))
	}
	if r.Parts > 1 {
		cov += fmt.Sprintf(", %d parts", r.Parts)
	}
	if r.GateFailed {
		cov += ", gate failed"
	}
	fmt.Fprintln(w, cov)

	if d := r.Diff; d != nil {
		if d.TotalLines == 0 {
			fmt.Fprintln(w, "diff coverage: no executable lines changed")
		} else {
			fmt.Fprintf(w, "diff coverage: %.1f%% (%d/%d changed lines)\n", d.Coverage, d.CoveredLines, d.TotalLines)
		}
		if len(d.Uncovered) > 0 {
			fmt.Fprintln(w, "uncovered changed lines:")
			for _, f := range d.Uncovered {
				fmt.Fprintf(w, "  %s: %s\n", f.Path, f.Lines)
			}
		}
		if len(d.UnmatchedFiles) > 0 {
			fmt.Fprintf(w, "changed files without coverage data: %s\n", strings.Join(d.UnmatchedFiles, ", "))
		}
	}
	fmt.Fprintf(w, "report: %s\n", r.ReportURL)
}

func short(sha string) string { return sha[:min(len(sha), 7)] }

// detectForge names the forge the repo lives on: the CI the CLI runs in,
// else the host of the origin remote. "" when neither says, as on a
// self-managed GitLab — -forge answers that.
func detectForge(env envFunc, git gitFunc) string {
	switch {
	case env("GITHUB_ACTIONS") == "true":
		return "github"
	case env("BITBUCKET_REPO_FULL_NAME") != "":
		return "bitbucket"
	case env("GITLAB_CI") == "true":
		return "gitlab"
	}
	remote, err := git("remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	for host, forge := range map[string]string{"github.com": "github", "gitlab.com": "gitlab", "bitbucket.org": "bitbucket"} {
		if strings.Contains(remote, host) {
			return forge
		}
	}
	return ""
}
