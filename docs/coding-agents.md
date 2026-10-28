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

## MCP

An agent that can run commands needs nothing more than `gocov report`. Clients that cannot, such as a chat app
or an editor's chat panel, read the same report over MCP as one tool, `get_report`, in one of two ways.

### As a connector, by URL

gocov serves MCP at `https://app.gocov.dev/mcp` (on a self-hosted instance, `/mcp` on its own URL). Add it as a
custom connector: in claude.ai, open *Settings → Connectors → Add custom connector* and enter

| field          | value                         |
|----------------|-------------------------------|
| Name           | `gocov`                       |
| MCP server URL | `https://app.gocov.dev/mcp`   |

Nothing to install. Connecting opens gocov's sign-in: sign in with your forge account as you would on the web,
and approve the connector. From then on it reads what you see in gocov, the repos of your workspaces, private ones
included, and [public reports](public-reports.md). It cannot change anything, and after 30 days it asks you to
sign in again. Since the client has no checkout, ask about a repo by name: *"what did gocov/gocov's last pull
request leave untested?"*

### As a local command

[`gocov mcp`](cli.md#serving-reports-over-mcp) runs on your machine as a stdio server, reads private repos too,
and finds the repo and commit from the checkout it starts in. Register it with a client that starts local
servers:

```sh
claude mcp add gocov -- gocov mcp
```

For clients configured through a JSON file, such as Claude Desktop or Cursor:

```json
{
  "mcpServers": {
    "gocov": { "command": "gocov", "args": ["mcp"] }
  }
}
```

Started inside a checkout, the tool needs no arguments: it reads the checked-out commit. Started anywhere else,
the agent passes `forge` and `repo`, plus `commit` or `branch`. For a private repo, add `"env": {"GOCOV_TOKEN":
"…"}` to the entry. The token is the same one described below.

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
