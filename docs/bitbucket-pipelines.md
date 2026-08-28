# Bitbucket Pipelines

1. Add `GOCOV_TOKEN` as a **secured workspace variable** holding your workspace's
   [upload token](getting-started.md). When self-hosting, add `GOCOV_SERVER` beside it.

2. Add the [gocov pipe](https://bitbucket.org/gocov/upload-pipe) after your tests:

```yaml
- step:
    script:
      - go test ./... -covermode=atomic -coverprofile=coverage.out
      - pipe: docker://gocov/upload-pipe:0
        variables:
          FILES: coverage.out
          TOKEN: $GOCOV_TOKEN
```

Commit, branch, repo and PR id are auto-detected. When self-hosting, add `SERVER: $GOCOV_SERVER` to the pipe's
variables. Only the test command and the `FILES:` path change for other languages —
[Languages & formats](languages.md) lists what each test tool writes.

## Without Docker

On runners without Docker (the pipe needs it), run the CLI directly instead:

```yaml
- step:
    script:
      - go test ./... -covermode=atomic -coverprofile=coverage.out
      - go run github.com/gocov/gocov/cmd/gocov@latest upload coverage.out
```

This needs a Go toolchain on the runner; otherwise download the prebuilt binary like
[any other CI](ci-other.md) does.

## Blocking merges on coverage

With the workspace [connected](connecting.md), every upload sets a build status on the commit and attaches a Code
Insights report to the PR. To make the [coverage gate](coverage-gate.md) block merges, require the `gocov` build in
the repo's merge checks — a FAILED status then blocks the PR.
