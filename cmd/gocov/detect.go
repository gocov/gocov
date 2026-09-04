package main

import (
	"cmp"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// buildInfo is the CI metadata attached to an upload.
type buildInfo struct {
	Repo   string
	Commit string
	Branch string
	PRID   string
}

// fill copies non-empty fields from other into b's empty ones, so
// detection sources stack in precedence order: whatever b already knows
// wins, and other only answers what is still open.
func (b *buildInfo) fill(other buildInfo) {
	b.Repo = cmp.Or(b.Repo, other.Repo)
	b.Commit = cmp.Or(b.Commit, other.Commit)
	b.Branch = cmp.Or(b.Branch, other.Branch)
	b.PRID = cmp.Or(b.PRID, other.PRID)
}

type envFunc func(key string) string
type gitFunc func(args ...string) (string, error)
type readFileFunc func(path string) ([]byte, error)

// detectBuild resolves build metadata from the CI environment — Bitbucket
// Pipelines, GitHub Actions or GitLab CI variables — falling back to git
// for anything missing.
func detectBuild(env envFunc, git gitFunc, readFile readFileFunc) buildInfo {
	b := buildInfo{
		Repo:   env("BITBUCKET_REPO_FULL_NAME"),
		Commit: env("BITBUCKET_COMMIT"),
		Branch: env("BITBUCKET_BRANCH"),
		PRID:   env("BITBUCKET_PR_ID"),
	}
	b.fill(githubBuild(env, readFile))
	b.fill(gitlabBuild(env))
	if b.Commit == "" {
		if out, err := git("rev-parse", "HEAD"); err == nil {
			b.Commit = out
		}
	}
	if b.Branch == "" {
		if out, err := git("rev-parse", "--abbrev-ref", "HEAD"); err == nil && out != "HEAD" {
			b.Branch = out
		}
	}
	if b.Repo == "" {
		if out, err := git("remote", "get-url", "origin"); err == nil {
			b.Repo = slugFromRemote(out)
		}
	}
	return b
}

// metaInfo is the upload provenance the CLI gathers from the CI environment
// and git — the commit subject and author, and a link back to the CI run.
type metaInfo struct {
	CommitMessage string
	CommitAuthor  string
	CIProvider    string
	CIRunURL      string
}

// detectMeta collects provenance for the resolved commit: its subject and
// author from git, and the CI provider and run URL from the environment.
// Every field is best-effort — a shallow clone or an unknown CI just yields
// empty strings.
func detectMeta(env envFunc, git gitFunc, commit string) metaInfo {
	ref := cmp.Or(commit, "HEAD")
	m := metaInfo{}
	if out, err := git("show", "-s", "--format=%s", ref); err == nil {
		m.CommitMessage = firstLine(out)
	}
	if out, err := git("show", "-s", "--format=%an", ref); err == nil {
		m.CommitAuthor = strings.TrimSpace(out)
	}
	switch {
	case env("GITHUB_ACTIONS") != "":
		m.CIProvider = "github"
		if srv, repo, run := env("GITHUB_SERVER_URL"), env("GITHUB_REPOSITORY"), env("GITHUB_RUN_ID"); srv != "" && repo != "" && run != "" {
			m.CIRunURL = srv + "/" + repo + "/actions/runs/" + run
		}
	case env("GITLAB_CI") != "":
		m.CIProvider = "gitlab"
		m.CIRunURL = cmp.Or(env("CI_JOB_URL"), env("CI_PIPELINE_URL"))
	case env("BITBUCKET_BUILD_NUMBER") != "":
		m.CIProvider = "bitbucket"
		if origin := env("BITBUCKET_GIT_HTTP_ORIGIN"); origin != "" {
			m.CIRunURL = origin + "/pipelines/results/" + env("BITBUCKET_BUILD_NUMBER")
		}
	}
	return m
}

// runInfo identifies the GitHub Actions workflow run an upload came from,
// for tokenless fork-PR uploads: the server verifies the run against
// GitHub instead of a token. HeadRepo is the fork the PR head lives on,
// read from the event payload.
type runInfo struct {
	EventName  string
	RunID      string
	RunAttempt string
	HeadRepo   string
}

// tokenlessEligible reports whether the environment is one the server's
// tokenless verification can vouch for: a GitHub Actions pull_request
// workflow — the one place CI legitimately has no token to send.
func (ri runInfo) tokenlessEligible() bool {
	return ri.EventName == "pull_request" && ri.RunID != ""
}

// detectGitHubRun reads the workflow-run identity from the GitHub Actions
// environment; zero outside Actions.
func detectGitHubRun(env envFunc, readFile readFileFunc) runInfo {
	if env("GITHUB_ACTIONS") == "" {
		return runInfo{}
	}
	return runInfo{
		EventName:  env("GITHUB_EVENT_NAME"),
		RunID:      env("GITHUB_RUN_ID"),
		RunAttempt: cmp.Or(env("GITHUB_RUN_ATTEMPT"), "1"),
		HeadRepo:   readGitHubEvent(env, readFile).PullRequest.Head.Repo.FullName,
	}
}

// firstLine returns the trimmed first line of s.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// githubPRRefRe extracts the PR number from GITHUB_REF, which is
// "refs/pull/{n}/merge" for pull_request workflow runs.
var githubPRRefRe = regexp.MustCompile(`^refs/pull/(\d+)/`)

// githubEvent is the slice of the Actions event payload the CLI reads: on
// pull_request runs, the PR number and its head — the SHA and branch a
// status can reach, and the fork the head lives on.
type githubEvent struct {
	PullRequest struct {
		Number int64 `json:"number"`
		Head   struct {
			SHA  string `json:"sha"`
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
	} `json:"pull_request"`
}

// readGitHubEvent parses the payload GITHUB_EVENT_PATH points at. It is
// zero when there is no payload or it cannot be read or parsed, so
// detection falls back to the environment alone.
func readGitHubEvent(env envFunc, readFile readFileFunc) githubEvent {
	var ev githubEvent
	path := env("GITHUB_EVENT_PATH")
	if path == "" || readFile == nil {
		return ev
	}
	data, err := readFile(path)
	if err != nil {
		return ev
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return githubEvent{}
	}
	return ev
}

// githubBuild reads GitHub Actions environment variables. For
// pull_request events GITHUB_SHA and GITHUB_REF_NAME describe the
// throwaway merge commit ("42/merge"), which no status or comment can
// reach — the event payload's head SHA and branch, and GITHUB_HEAD_REF,
// take precedence for those runs.
func githubBuild(env envFunc, readFile readFileFunc) buildInfo {
	if env("GITHUB_ACTIONS") == "" {
		return buildInfo{}
	}
	b := buildInfo{
		Repo:   env("GITHUB_REPOSITORY"),
		Commit: env("GITHUB_SHA"),
		Branch: cmp.Or(env("GITHUB_HEAD_REF"), env("GITHUB_REF_NAME")),
	}
	if m := githubPRRefRe.FindStringSubmatch(env("GITHUB_REF")); m != nil {
		b.PRID = m[1]
	}
	if pr := readGitHubEvent(env, readFile).PullRequest; pr.Head.SHA != "" {
		b.Commit = pr.Head.SHA
		b.Branch = cmp.Or(pr.Head.Ref, b.Branch)
		if pr.Number > 0 {
			b.PRID = strconv.FormatInt(pr.Number, 10)
		}
	}
	return b
}

// gitlabBuild reads GitLab CI environment variables. CI_COMMIT_BRANCH is
// empty in merge request pipelines, where the source branch name carries
// the branch instead. The known trap (a cousin of GitHub's merge-commit
// trap): in merged-results pipelines CI_COMMIT_SHA points at a transient
// merged commit no status or comment can reach — when
// CI_MERGE_REQUEST_SOURCE_BRANCH_SHA is set, it names the real head and
// wins.
func gitlabBuild(env envFunc) buildInfo {
	if env("GITLAB_CI") == "" {
		return buildInfo{}
	}
	return buildInfo{
		Repo:   env("CI_PROJECT_PATH"),
		Commit: cmp.Or(env("CI_MERGE_REQUEST_SOURCE_BRANCH_SHA"), env("CI_COMMIT_SHA")),
		Branch: cmp.Or(env("CI_COMMIT_BRANCH"), env("CI_MERGE_REQUEST_SOURCE_BRANCH_NAME")),
		PRID:   env("CI_MERGE_REQUEST_IID"),
	}
}

// remoteSlugRe extracts "workspace/repo" from SSH and HTTPS remote URLs:
// git@bitbucket.org:acme/widgets.git, https://bitbucket.org/acme/widgets.git,
// https://user@github.com/acme/widgets.
var remoteSlugRe = regexp.MustCompile(`[:/]([^/:]+/[^/:]+?)(?:\.git)?/?$`)

func slugFromRemote(remote string) string {
	m := remoteSlugRe.FindStringSubmatch(strings.TrimSpace(remote))
	if m == nil {
		return ""
	}
	return m[1]
}

// runGit is the production gitFunc.
func runGit(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// moduleFromGoMod reads the module path from a go.mod file, so the server
// can map module-qualified profile paths to repo-relative diff paths.
// Returns "" when the file is missing or has no module directive.
func moduleFromGoMod(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module"); ok {
			rest = strings.TrimSpace(rest)
			if rest == "" {
				continue
			}
			return strings.Trim(rest, `"`)
		}
	}
	return ""
}
