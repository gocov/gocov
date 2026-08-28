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

Returns `201` with `{id, total_pct, covered_stmts, total_stmts,
delta_pct, build_status}`. Uploads carrying a `pr_id` additionally get
`diff_pct`, `diff_covered_lines`, `diff_total_lines`, `diff_status` and
`pr_comment` when the repo's workspace is [connected to its forge](connecting.md).

## Badge

```markdown
![coverage](https://app.gocov.dev/badge/myworkspace/myrepo.svg)
```

A self-hosted instance serves the same path from its own host. Either way there is nothing to assemble by hand: the
repo page shows the finished snippet with a copy button.

Red below 50%, yellow 50–75%, green above 75%. Shows the latest upload on the repo's default branch. Badges are served
without authentication even when web UI sign-in is enabled.
