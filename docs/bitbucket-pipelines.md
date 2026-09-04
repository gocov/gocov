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

## Uploading without a token

Bitbucket Pipelines can hand a step a short-lived, signed OIDC identity token, so you can drop the `GOCOV_TOKEN`
variable entirely. Name your gocov server under the step's `oidc.audiences` — that both turns OIDC on and binds
the token to your server so it can't be replayed elsewhere. The uploader picks the token up from the step's
environment and the server verifies which repository it came from.

```yaml
- step:
    oidc:
      audiences:
        - https://app.gocov.dev   # your gocov server URL
    script:
      - go test ./... -covermode=atomic -coverprofile=coverage.out
      - go run github.com/gocov/gocov/cmd/gocov@latest upload coverage.out
```

The audience must equal your gocov server's URL (`https://app.gocov.dev` on the hosted service, your instance's
URL when self-hosting). Bitbucket appends it to the default workspace audience, so the token can carry your
server alongside any other resource servers the pipeline already targets.

The repository's Bitbucket workspace must already be registered on gocov and [connected](connecting.md) to
Bitbucket — the same connection that posts the build status and Code Insights report. The repository itself needs
no setup: its first OIDC upload registers it, exactly as a token upload would. OIDC replaces only the upload token;
publishing still goes through that connection. A pasted `GOCOV_TOKEN` always takes precedence, so existing setups are
untouched, and a rejected OIDC upload logs the reason and exits 0 rather than failing the build.

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
