# Cloudflare CI

[Cloudflare CI](https://github.com/cloudflare/ci) (`@cloudflare/ci`, announced
[August 2026](https://blog.cloudflare.com/ci-workflows)) runs CI pipelines on
Cloudflare Workflows: the pipeline is a TypeScript class deployed as a Worker,
and each step is a shell command executed in a sandbox container. Because steps
are plain shell commands, the gocov CLI works unmodified — only two things need
care: the token travels as a step-scoped Worker secret, and repo/commit/branch
are passed explicitly (the sandbox workspace has no `.git` and gocov has no
auto-detection for Cloudflare CI).

A complete deployable example lives in the Cloudflare CI repo under
[`examples/`](https://github.com/cloudflare/ci/tree/main/examples); the
essentials are below.

## Token

Store the repo's upload token as a secret on your CI Worker:

```sh
pnpm exec wrangler secret put GOCOV_TOKEN
```

The `secrets` option on a runner resolves named Worker secrets into that step's
command env only — the install and test steps never see the token. When
self-hosting, pass `-server https://gocov.example` on the upload command (or a
`GOCOV_SERVER` var the same way); the default is the hosted service.

## Pipeline

Add the upload as a runner chained after the test step, so it inherits the
workspace snapshot containing `cover.out`:

```ts
export class CI extends CIWorkflow<CloudflareArtifacts, Bindings> {
  protected async pipeline(
    event: WorkflowEvent<CiParams<CloudflareArtifacts>>,
    _step: WorkflowStep,
    ci: CiContext
  ): Promise<void> {
    const { owner, repo, sha, branch } = event.payload;

    const deps = await ci.runner({
      name: 'install',
      command: 'go mod download',
      cache: { inputs: ['go.mod', 'go.sum'] },
    });

    const tested = await deps.runner({
      name: 'test',
      command: 'go test ./... -covermode=atomic -coverprofile=cover.out',
    });

    await tested.runner({
      name: 'coverage',
      command:
        'go run github.com/gocov/gocov/cmd/gocov@v0.11.0 upload' +
        ' -repo "$UPLOAD_REPO" -commit "$UPLOAD_COMMIT" -branch "$UPLOAD_BRANCH" cover.out',
      env: {
        UPLOAD_REPO: `${owner}/${repo}`,
        UPLOAD_COMMIT: sha,
        UPLOAD_BRANCH: branch ?? '',
      },
      secrets: ['GOCOV_TOKEN'],
    });
  }
}
```

Repo, commit and branch come from the typed pipeline event and travel through
`env` rather than string interpolation, so a branch name can never be
interpreted by the shell. `-repo` must be the gocov slug (`workspace/repo`)
under your workspace prefix. An empty `-branch` is fine — the server falls back
to the repo's default branch.

The sandbox base image ships no Go toolchain; extend it in the example's
`Dockerfile`:

```dockerfile
FROM docker.io/cloudflare/sandbox:0.12.1

ARG GO_VERSION=1.27.0
ARG GO_SHA256=675c26c449cbb18fc24b74650de1eabbae6e16f64326fd85a283fb3b58280685
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tgz \
  && echo "${GO_SHA256}  /tmp/go.tgz" | sha256sum -c - \
  && tar -C /usr/local -xzf /tmp/go.tgz && rm /tmp/go.tgz
ENV PATH=/usr/local/go/bin:$PATH

# Workspace Snapshots capture /workspace only; keeping Go's caches inside it
# lets the cache-keyed install runner restore the module cache on later runs.
ENV GOMODCACHE=/workspace/.cache/go-mod GOCACHE=/workspace/.cache/go-build
```

Non-Go projects skip the toolchain install and upload with a
[prebuilt binary](ci-upload.md#prebuilt-binaries) instead; the coverage format
is detected from the file content as usual.

## What works, what doesn't

Cloudflare CI currently triggers only on `cf.artifacts.repo.pushed`, and
Cloudflare Artifacts has no pull-request concept. That bounds the integration:

- **Works** — per-commit coverage: totals, history, parts, the badge, and the
  coverage gate result in the upload response (`-fail-on-gate` fails the step).
- **Doesn't apply** — everything that hangs off a PR: PR comments and
  diff-coverage annotations.
- **Build statuses** are pushed to the forge repo the gocov workspace tracks.
  If the Artifacts repository mirrors a forge repo, the commit SHAs match and
  statuses land on the forge commits as usual; for source that lives only in
  Artifacts there is no forge commit to attach them to, and the upload
  response reports the status push as failed while the coverage itself is
  stored fine.

Cloudflare has push events from other sources on its roadmap; PR-shaped
features can be revisited when an event source with PRs exists.

## See also

- [Uploading from CI](ci-upload.md) — the other CI providers, prebuilt
  binaries, other coverage ecosystems
- [Parts](parts.md) — combining several uploads into one report per commit
