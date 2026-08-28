# GitHub Actions

Add [`gocov/gocov-action`](https://github.com/marketplace/actions/gocov-coverage-upload) after your test step:

```yaml
- run: go test ./... -covermode=atomic -coverprofile=coverage.out
- uses: gocov/gocov-action@v1
  with:
    files: coverage.out
    token: ${{ secrets.GOCOV_TOKEN }}
```

Set `GOCOV_TOKEN` as an **organization secret** (or a repository secret) holding your workspace's
[upload token](getting-started.md).

That's the whole setup:

- **No toolchain needed.** The action downloads the pinned CLI binary for the runner and verifies its checksum, so the
  same three lines work whatever language your tests are written in — only the `files:` path changes.
  [Languages & formats](languages.md) lists what each test tool writes.
- **Everything is auto-detected**: commit, branch, repo and PR number, including the PR head SHA on `pull_request`
  runs.
- `files` takes a comma-separated list and globs.

## Options

| input    | meaning                                                                                      |
|----------|----------------------------------------------------------------------------------------------|
| `files`  | coverage file(s) to upload — comma-separated, globs allowed                                  |
| `token`  | the workspace upload token                                                                   |
| `part`   | names one slice of a matrix or multi-job build — see [Parts](parts.md)                       |
| `server` | only when self-hosting: your instance's URL. On the hosted service the server is implicit    |

## Without the action

On a runner that already has Go, the CLI also runs straight from source — no marketplace dependency:

```yaml
- run: go run github.com/gocov/gocov/cmd/gocov@latest upload coverage.out
  env:
    GOCOV_TOKEN: ${{ secrets.GOCOV_TOKEN }}
```

Or download the prebuilt binary like [any other CI](ci-other.md) does.

## Blocking merges on coverage

With the workspace [connected](connecting.md), every upload sets a commit status and a check run on the PR. To make
the [coverage gate](coverage-gate.md) block merges, require `gocov` or `gocov coverage` under
**Settings → Branches → Require status checks to pass**.
