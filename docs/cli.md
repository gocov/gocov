# CLI reference

```
gocov upload [flags] <profile file>
gocov version
```

The CLI uploads one coverage file per invocation, auto-detecting everything it can: the format from the file's
content, and repo, commit, branch and PR id from Bitbucket Pipelines, GitHub Actions or GitLab CI environment
variables, falling back to `git`.

## Installing

- [GitHub Actions](github-actions.md) and [Bitbucket Pipelines](bitbucket-pipelines.md) have wrappers that install it
  for you.
- Prebuilt binaries for every platform are on
  [GitHub Releases](https://github.com/gocov/gocov/releases) — see [Other CI systems](ci-other.md) for a pinned,
  checksum-verified download recipe.
- With a Go toolchain: `go run github.com/gocov/gocov/cmd/gocov@latest upload …`

## Flags

| flag           | default                          | meaning                                                                                        |
|----------------|----------------------------------|------------------------------------------------------------------------------------------------|
| `-token`       | `$GOCOV_TOKEN`                   | the workspace/repo upload token. Optional when the CI job can mint an OIDC identity token, or in a GitHub Actions `pull_request` run — see [without a token](#uploading-without-a-token) |
| `-server`      | `$GOCOV_SERVER`, else the hosted service | the gocov instance to upload to — only needed when self-hosting                        |
| `-repo`        | auto-detect                      | repo slug, `workspace/repo`                                                                    |
| `-commit`      | auto-detect                      | commit SHA. The one value that has no fallback: if detection fails, the upload asks for it     |
| `-branch`      | auto-detect                      | branch name                                                                                    |
| `-pr`          | auto-detect                      | pull request id; enables [diff coverage](pull-requests.md)                                     |
| `-format`      | detect from content              | `go`, `lcov`, `jacoco`, `cobertura`, `clover` or `simplecov`                                   |
| `-path-prefix` | from `go.mod` for Go profiles    | prefix mapping profile paths to repo paths, e.g. the Go module path — see below                |
| `-part`        | `$GOCOV_PART`, else `default`    | names this slice of the commit's coverage when several jobs upload — see [Parts](parts.md)     |
| `-ignore`      | `$GOCOV_IGNORE`, else none       | leave matching files out of the report, e.g. `cmd/preview/**`; repeatable or comma-separated — see [Ignoring files](ignoring-files.md) |
| `-fail-on-gate`| off                              | exit non-zero when the server reports a failed [coverage gate](coverage-gate.md)               |

## Environment variables

| variable       | equivalent flag |
|----------------|-----------------|
| `GOCOV_TOKEN`  | `-token`        |
| `GOCOV_SERVER` | `-server`       |
| `GOCOV_PART`   | `-part`         |
| `GOCOV_IGNORE` | `-ignore`       |

`GOCOV_PART` is handy for matrix jobs that already expose the variant in the environment. `GOCOV_IGNORE` takes
several patterns separated by commas or newlines. Flags win over the environment.

## Uploading without a token

When no `-token`/`$GOCOV_TOKEN` is set, the CLI tries two secret-free paths in order before giving up:

1. **OIDC.** If the CI job can provide an OIDC identity token — GitHub Actions with `permissions: id-token: write`,
   Bitbucket Pipelines with `oidc.audiences`, or GitLab CI with an `id_tokens` entry named `GOCOV_ID_TOKEN` — the
   CLI sends it in place of the token, scoped to the gocov server's audience. The server verifies the forge's
   signature and the repository the token names, so the upload is fully verified — not marked unverified. This is
   the recommended setup for a repo's own `push` and same-repo PR builds; see
   [GitHub Actions](github-actions.md#uploading-without-a-token),
   [Bitbucket Pipelines](bitbucket-pipelines.md#uploading-without-a-token) and
   [GitLab CI](gitlab-ci.md#uploading-without-a-token).
2. **Tokenless fork-PR** (below), for the fork pull request that has neither a secret nor an `id-token`.

An explicit token always wins, so existing setups are untouched. A rejected OIDC upload does not fail the
build: the CLI prints one line with the server's reason (`gocov: OIDC upload rejected — …`) and exits 0.

## Tokenless fork-PR uploads

In a GitHub Actions `pull_request` workflow with no token set — the fork-PR situation, where secrets are withheld —
the CLI uploads tokenless: it sends the workflow run's identity (run id, attempt, PR number, head SHA, fork) and the
server [verifies the run with GitHub](pull-requests.md#fork-prs-without-a-token) through the repo's App installation.
Works on public repos with the gocov GitHub App connected; anywhere else a missing token stays an error.

In tokenless mode an upload that is refused or fails does **not** fail the build: the CLI prints one line with the
server's reason (`gocov: tokenless upload rejected — …`) and exits 0.

## `-path-prefix`

Diff coverage matches the paths in the profile against the paths in the PR's diff. When the profile records paths
under a prefix the repo doesn't have — a Go module path, a CI checkout directory — set `-path-prefix` to that prefix
so the two line up. For Go profiles the CLI fills it from `go.mod` automatically; JaCoCo's package-qualified paths are
matched by suffix and usually need nothing. Symptoms and details:
[Why coverage changed](coverage-changed.md).

## Output and exit code

A successful upload prints the totals the server computed — coverage, delta, how many files the
[ignore patterns](ignoring-files.md) dropped (when any did), diff coverage, and the status of each forge surface
(`build status`, `pr comment`, `code insights`: `posted`, or `skipped` with the reason) plus the gate verdict:

```
uploaded: 82.0% (1230/1500 statements), delta +0.4%
ignored: 3 files
diff coverage: 91.7% (22/24 changed lines)
build status: posted
pr comment: posted
gate: passed
```

The exit code is non-zero on any upload error, and — only with `-fail-on-gate` — on a failed gate, which is how a
pipeline step turns the gate into a hard failure even without [forge-side merge blocking](coverage-gate.md).

The raw HTTP endpoint underneath, for anything the CLI doesn't cover, is documented in [API & badge](api.md).
