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

## Uploading without a token

On your own repository's `push` and same-repo pull request builds you can drop the `GOCOV_TOKEN` secret
altogether. Grant the workflow the `id-token: write` permission and the uploader asks GitHub for a
short-lived, signed identity token that proves which repository the run belongs to; the server verifies it
against GitHub and accepts the upload. Nothing to create in the settings UI, nothing to paste, nothing to
rotate.

```yaml
permissions:
  contents: read
  id-token: write
steps:
  - run: go test ./... -covermode=atomic -coverprofile=coverage.out
  - run: go run github.com/gocov/gocov/cmd/gocov@latest upload coverage.out
```

The repository must already be tracked in a workspace [connected](connecting.md) through the gocov GitHub
App — the same connection that posts the PR comment and check run. OIDC replaces only the *upload* token;
publishing still goes through that App identity, so the reported status looks exactly like a token upload
(it is **not** marked unverified).

A few things follow from how the identity token works:

- The token's audience is bound to your gocov server, so a token minted for another service can't be replayed
  here. When self-hosting, pass `-server`/`$GOCOV_SERVER` so the audience matches your instance.
- If the `id-token` permission is missing, the uploader falls back to a pasted `GOCOV_TOKEN`, then to
  [tokenless fork-PR verification](#fork-pull-requests) — the precedence never surprises an existing setup.
- A rejected OIDC upload never fails the build: the job logs the reason and exits 0.

The pasted-token setup above keeps working unchanged; OIDC is simply the recommended way to start a new repo.

## Options

| input    | meaning                                                                                      |
|----------|----------------------------------------------------------------------------------------------|
| `files`  | coverage file(s) to upload — comma-separated, globs allowed                                  |
| `token`  | the workspace upload token                                                                   |
| `part`   | names one slice of a matrix or multi-job build — see [Parts](parts.md)                       |
| `server` | only when self-hosting: your instance's URL. On the hosted service the server is implicit    |

## Fork pull requests

Workflow runs for PRs opened from forks cannot read `secrets.GOCOV_TOKEN` — GitHub withholds secrets from fork code.
On a **public** repo whose workspace is [connected through the gocov GitHub App](connecting.md) that's fine: with no
token available the uploader switches to tokenless mode and the server verifies the workflow run itself through the
App. Fork contributors see the PR comment and check run without any setup;
[Coverage in pull requests](pull-requests.md#fork-prs-without-a-token) has the details and limits.

A tokenless upload never fails the build: if it is refused (App not installed, private repo, verification failed),
the job logs the reason and exits 0.

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
