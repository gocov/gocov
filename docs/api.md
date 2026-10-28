# API & badge

Most uploads should go through the [CLI](cli.md) or its CI wrappers; this is the HTTP endpoint underneath, for
anything they don't cover.

## Upload

`POST /api/v1/upload` — multipart form, `Authorization: Bearer <token>`

| part          | meaning                                                                                                                                                                                                                                                         |
|---------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `profile`     | file: the coverage profile                                                                                                                                                                                                                                      |
| `repo`        | optional; must match the token's repo                                                                                                                                                                                                                           |
| `commit`      | required commit SHA                                                                                                                                                                                                                                             |
| `branch`      | defaults to the repo's default branch                                                                                                                                                                                                                           |
| `pr_id`       | optional pull request id                                                                                                                                                                                                                                        |
| `format`      | `go`, `lcov`, `jacoco`, `cobertura`, `clover` or `simplecov`; omitted → detected from content                                                                                                                                                                   |
| `path_prefix` | maps profile paths to repo paths for diff coverage, e.g. the Go module path (the CLI fills it from go.mod)                                                                                                                                                      |
| `part`        | optional; names one slice of the commit's coverage (`backend`, `frontend`, `e2e`, …) uploaded from a separate CI job. Normalized to a lowercase slug (`[a-z0-9._-]`, ≤64); omitted or blank → `default`. Re-uploading a part replaces it. See [Parts](parts.md) |
| `ignore`      | optional, repeatable; a glob pattern (or a comma/newline-separated list) for files to leave out of this upload, applied on top of the repo's own patterns. At most 100 patterns of 200 characters; an invalid one is a `400`. See [Ignoring files](ignoring-files.md)                |

Returns `201` with `{id, total_pct, covered_stmts, total_stmts,
delta_pct, build_status, part, parts}` — the totals and delta are the commit's merged report, `part` is this upload's
normalized [part](parts.md) name and `parts` lists, sorted, every part the totals were merged from so far — plus
`ignored_files` when ignore patterns dropped any and `warnings`, a list of
sentences, when the commit's merged report was built with a caveat. Uploads carrying a `pr_id` additionally get
`diff_pct`, `diff_covered_lines`, `diff_total_lines`, `diff_status` and
`pr_comment` when the repo's workspace is [connected to its forge](connecting.md).

### OIDC uploads

A request **without** the `Authorization` header may instead carry a forge-minted OIDC identity token in an
`oidc_token` form part. The server verifies the token's signature against the forge's published keys and checks:

- the issuer is one it trusts (GitHub Actions, Bitbucket Pipelines, and GitLab CI — plus any self-managed GitLab
  issuers the operator configured);
- the audience equals this server's URL — a token minted for another audience is rejected;
- the repository claim falls under a registered workspace on that forge, and the request's own `repo` part (if
  sent) agrees with it. A repo not yet tracked is registered by this upload, as a workspace token's first upload
  would register it; the workspace itself must already exist.

On success the upload proceeds like a token upload — fully verified, published through the workspace's forge
connection. Refusals are explicit: `403 oidc_bad_audience`, `403 oidc_unknown_issuer`, `403 oidc_repo_mismatch`,
`401 oidc_invalid_token`, and `404` for a repo under no registered workspace. See
[GitHub Actions](github-actions.md#uploading-without-a-token).

### Tokenless fork-PR uploads

A request **without** the `Authorization` header or an `oidc_token` may instead authenticate as a running GitHub Actions `pull_request`
workflow — the [fork-PR path](pull-requests.md#fork-prs-without-a-token). It carries three extra parts, and `repo`,
`commit` (the PR head SHA) and `pr_id` become required:

| part          | meaning                                                          |
|---------------|------------------------------------------------------------------|
| `run_id`      | `$GITHUB_RUN_ID` of the workflow run doing the upload            |
| `run_attempt` | `$GITHUB_RUN_ATTEMPT`                                            |
| `head_repo`   | the fork the PR head is on (`pull_request.head.repo.full_name`)  |

The server verifies the claim with GitHub through the repo's App installation; the repo must be public, tracked, and
its workspace connected. Refusals are explicit: `403` with the reason, `404` for an untracked repo, `409` when the
same `(run_id, run_attempt, part)` already uploaded, `429` past the per-repo hourly limit.

## Reading a report

`GET /api/v1/reports/{forge}/{workspace}/{repo}` — a commit's merged report, for tools and coding agents: what a
change left untested, without scraping the report page. The
[`gocov report`](cli.md#reading-a-report) command calls it for you.

Without a credential it answers for repos whose [reports are public](public-reports.md) (and for every repo on a
self-hosted instance without sign-in). Any private repo is read with what its upload authenticates with, sent as
`Authorization: Bearer <credential>`:

- the repo's upload token, or its workspace's token;
- in CI, the forge's [OIDC identity token](#oidc-uploads) for this server, which names the repo it was minted
  for.

Anything else gets `404`, the same `404` as for a repo that does not exist, and so does a credential for
another repo. A credential that is not one at all (an unknown token, or an OIDC token that does not verify) gets
`401` or `403` with the reason, as an upload would.

| query    | meaning                                                                                           |
|----------|---------------------------------------------------------------------------------------------------|
| `commit` | the commit SHA, as it was uploaded                                                                |
| `branch` | without `commit`: the newest report on this branch. Without either: the repo's default branch     |

```json
{
  "repo": {"forge": "github", "slug": "acme/widgets"},
  "commit": "7aa8f0162ab5…", "branch": "feature", "pr_id": "247",
  "updated_at": "2026-10-28T08:11:36Z",
  "parts": 2,
  "coverage": 88.04, "covered_statements": 5765, "total_statements": 6550,
  "gate_failed": false,
  "base": {"commit": "c54dc2c0b7d6…", "coverage": 87.65}, "delta": 0.39,
  "diff": {
    "coverage": 82.6, "covered_lines": 19, "total_lines": 23,
    "uncovered": [
      {"path": "cmd/gocov/oidc.go", "lines": "49-50, 57-58", "spans": [[49, 50], [57, 58]]}
    ],
    "unmatched_files": []
  },
  "report_url": "https://app.gocov.dev/uploads/1114?parts=merged"
}
```

- The numbers are the commit's report with every [part](parts.md) merged; `parts` counts them, and `updated_at` is
  the latest upload, so poll until the parts you expect have arrived.
- `base` and `delta` compare with the report the commit is measured against, as on the report page; both are `null`
  when there is nothing to compare with.
- `diff` is the [diff coverage](pull-requests.md) of a pull-request commit, `null` otherwise. `uncovered` lists
  only the changed files with changed lines no test ran, as the human `lines` and as inclusive `[start, end]`
  `spans`; `unmatched_files` are changed files the coverage data does not mention.
- `404` also answers a commit with no report yet, or a branch with none.

Fields are only ever added: a client should ignore the ones it does not know.

## MCP

`/mcp` serves the read API as a remote [MCP](https://modelcontextprotocol.io) server over Streamable HTTP, for
clients that are given a URL rather than a command to run. [Coding agents](coding-agents.md#as-a-connector-by-url)
shows how to add it. It offers one tool, `get_report`:

| argument | meaning                                                                              |
|----------|--------------------------------------------------------------------------------------|
| `forge`  | required: `github`, `gitlab` or `bitbucket`                                          |
| `repo`   | required: the repo slug, `workspace/repo`                                            |
| `commit` | the commit SHA. Without it, the newest report on `branch`                            |
| `branch` | the branch to read the newest report on; the repo's default branch when left out     |
| `format` | `text` (default), the lines [`gocov report`](cli.md#reading-a-report) prints, or `json`, the answer above |

On an instance without sign-in it answers for every repo, to anyone. With sign-in, its client signs in first, as
the [MCP authorization spec](https://modelcontextprotocol.io/specification/latest/basic/authorization) has it: a
message without an access token gets `401` with a `WWW-Authenticate` header that points at
`/.well-known/oauth-protected-resource/mcp`, which names this server as the authorization server. The client
registers itself at `/mcp/register`, sends its user to `/mcp/authorize`, where they sign in to gocov as they
always do and approve it, and exchanges the code for a token at `/mcp/token`, under PKCE (`S256`). The token reads
what its user may see on the report pages, the repos of their workspaces and public reports, and nothing else:
it changes nothing, and it opens no web session. It lasts 30 days, then the client signs in again.

A repo the caller may not read gets the same answer as one that does not exist. The transport keeps no sessions.
`POST` carries one JSON-RPC message, a notification gets `202`, and `GET` gets `405`, since it opens no stream.

## Badge

```markdown
[![coverage](https://app.gocov.dev/badge/github/myworkspace/myrepo.svg)](https://app.gocov.dev/repos/github/myworkspace/myrepo?ref=badge)
```

The link lands on the repo's report page — for a public repo that is a [page anyone can read](public-reports.md);
on a private repo, visitors get the sign-in page. The path names the forge first (`github`, `gitlab` or
`bitbucket`), then the workspace and repo as the forge spells them: a project mirrored to two forges has two report
pages. A self-hosted instance serves the same paths from its own host. Either way there is nothing to assemble by
hand: the repo page shows the finished snippet with a copy button.

Red below 50%, yellow 50–75%, green above 75%. Shows the latest upload on the repo's default branch. Badges are served
without authentication even when web UI sign-in is enabled.
