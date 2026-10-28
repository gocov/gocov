# Coding agents

A coding agent that opens a pull request can also close its coverage gap. When CI has uploaded the commit,
[`gocov report`](cli.md#reading-a-report) prints the totals and the changed lines no test ran. The agent writes
tests for those lines, pushes, and reads the report again. It needs no browser and no scraping, and it works the
same for a person at a terminal.

```
$ gocov report
github/acme/widgets @ 7aa8f01 (feature, PR 247)
coverage: 88.0% (+0.4% vs c54dc2c), 2 parts
diff coverage: 82.6% (19/23 changed lines)
uncovered changed lines:
  cmd/gocov/oidc.go: 49-50, 57-58
report: https://app.gocov.dev/uploads/1114?parts=merged
```

## The loop

1. Push the change and let CI run the tests and upload coverage, every [part](parts.md) of it.
2. Run `gocov report` in the repo. It finds the forge, repo and commit from `git`, the way an upload does.
3. Write tests that run each line listed under *uncovered changed lines*, push, and go back to 1. Stop when the
   list is empty or diff coverage meets your [gate](coverage-gate.md).

`gocov report -json` gives the same answer as data: the [read API's JSON](api.md#reading-a-report), where each
uncovered file carries its lines as `[start, end]` spans. A report is only complete once every part has arrived.
Until then, `parts` and `updated_at` change from one read to the next.

## Telling the agent

Most agents read a file of standing instructions from the repo, such as `AGENTS.md` or `CLAUDE.md`. Add a section
like this one:

```markdown
## Test coverage

After pushing a pull request, wait for its CI to finish, then run `gocov report`.
For every file under "uncovered changed lines", add tests that run those lines,
push, and run `gocov report` again until none are left. `gocov report -json`
gives the same answer as JSON.
```

The CLI has to be on the agent's machine: see [installing](cli.md#installing). With a Go toolchain,
`go run github.com/gocov/gocov/cmd/gocov@latest report` needs no install.

## Private repos

A [public report](public-reports.md) needs no credential. A private repo is read with the credential its upload
uses:

- **In the repo's own CI** there is nothing to set up when the job can mint an OIDC identity token, the same token
  that lets the upload run [without a token](cli.md#uploading-without-a-token). On GitHub Actions, grant
  `id-token: write`. The token names the one repo it was minted for, so with it a job reads its own repo's
  private reports and no other's.
- **Anywhere else**, such as an agent on a laptop, set `GOCOV_TOKEN` to the workspace's
  [upload token](getting-started.md). Treat it as the secret it is: it also uploads coverage, and it reads
  every repo in the workspace.

Without a credential, or with one for another repo, a private repo answers as if it had no report, and
`gocov report` says which of the two cases it is.
