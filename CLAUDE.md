# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

gocov is a self-hostable coverage-tracking service (Coveralls/Codecov alternative): a single Go binary + Postgres, AGPL-3.0. Direct dependencies are pgx and caarlos0/env (tag-based env parsing, itself dependency-free); everything else is stdlib.

## Commands

```sh
go build ./...
go vet ./...
go test ./...                       # unit tests only — no Postgres needed
go test ./internal/server -run TestUpload   # single test
```

Postgres integration tests (in `internal/store/postgres`) are skipped unless `GOCOV_TEST_DATABASE_URL` is set; each test creates and drops its own scratch database via `internal/testpg`:

```sh
docker run --rm -d --name gocov-test-db -p 5433:5432 \
  -e POSTGRES_USER=gocov -e POSTGRES_PASSWORD=gocov -e POSTGRES_DB=gocov postgres:18-alpine
GOCOV_TEST_DATABASE_URL=postgres://gocov:gocov@localhost:5433/gocov go test ./...
```

For eyeballing UI changes without Postgres or OAuth, run the dev harness: `go run ./cmd/gocov-preview` serves the web UI from an in-memory store seeded with synthetic history (`GOCOV_PREVIEW_AUTH=1` adds fake sign-in so login/registration/settings pages are previewable). `docker compose up` runs the real thing.

CI (`.github/workflows/ci.yml`) runs vet + tests with a Postgres service; there is no separate linter.

## Architecture

Three binaries in `cmd/`:
- `gocov-server` — API + web UI. Configured entirely via environment variables (`DATABASE_URL`, `GOCOV_MODE=hosted`, OAuth keys per forge, `GOCOV_SECRET_KEY`, GitHub App vars, …).
- `gocov` — the upload CLI users run in CI. Detects the coverage format from file content (`detect.go`); defaults to the hosted server URL in `internal/hosted`.
- `gocov-preview` — throwaway dev harness, not part of the product.

Everything hangs off four interfaces, each with a production implementation and a test double, so handlers are fully testable without Postgres or a real forge:

| Interface | Production | Test double |
|---|---|---|
| `store.Store` (normalized coverage model, workspaces/repos/gates) | `store/postgres` | `store/memory` |
| `forge.Forge` (build statuses, PR comments, check runs) | `forge/{bitbucket,github,gitlab}` | `forge/fake` |
| `blobstore.Store` (raw uploaded profiles) | `blobstore/postgres` | `blobstore/memory` |
| `profile.Parser` (one per format: go, lcov, jacoco, cobertura, clover, simplecov) | `internal/profile` | — |

New formats, forges, or storage backends slot in behind these interfaces; no forge-specific types or URLs may leak out of the concrete forge implementations.

`internal/core` is the coverage logic, with no HTTP in it: `Accept` runs an upload through the whole pipeline — store the raw profile, measure diff coverage against the PR, evaluate the gate, persist the rows, merge the commit's "parts" (multiple uploads per commit) into a per-commit merged report, then push status/PR comment/check run to the forge. `gate.go`, `merge.go`, `publish.go` and `repo.go` are the pieces it is made of. `forges.go` owns the deployment's forge connections and their upkeep — resolving the client a repo's workspace is connected through, refreshing a grant and persisting the rotated refresh token, caching access tokens in memory, and marking a connection broken when the forge says it is gone. The package imports neither `net/http` nor `html/template` and `TestCoreImportsNoTransport` fails the build if that changes: anything needing a request or a template belongs on the other side of the line.

`internal/server` is the transport and the web UI around it: the HTTP API, the SVG badge, and an htmx-based web UI with Go templates and static assets embedded via `go:embed`. `upload.go` authenticates the token, reads and validates the multipart request, and hands a `core.Submission` to the pipeline, which resolves its own forge client. The connect flows (`githubapp.go`, `bitbucketgrant.go`, `gitlabgrant.go`, `githubwebhook.go`) are the handlers around those connections — consent redirects, callbacks, disconnect — while the tokens behind them live in `core`. Files are named for what they serve — one page or concern each (`page`-style files like `repopage.go`/`uploadpage.go`, `oauth.go`/`session.go`/`scope.go` for identity, `workspace.go`/`register.go`/`repo_settings.go` for administration) — and each has a test file of the same name. The exceptions are deliberate: `upload_gate_test.go`, `upload_merge_test.go` and `upload_push_test.go` drive the upload endpoint end to end for behaviour whose unit tests live in `internal/core`.

`internal/diffcov` computes diff coverage against forge-fetched diffs. `internal/auth` handles OAuth sign-in per forge; `internal/secretbox` encrypts stored grant refresh tokens (requires `GOCOV_SECRET_KEY`).

Every environment variable each binary reads is declared as a tagged struct field in `internal/config` (`Server`, `CLI`, `Preview`) — that package is the authoritative list, and `TestConfigurationDocIsInSync` fails the build if `docs/configuration.md` drifts from it (added, removed or re-defaulted variables). `main` parses and validates once at boot (`config.LoadServer`) and then only touches the struct; add new variables there, never as a fresh `os.Getenv` at the point of use. Tags cover reading, defaults, types and presence (`DATABASE_URL` is `required,notEmpty` — `required` alone would admit an empty string); what the tag vocabulary cannot say lives beside them, in `validate` for fatal rules (secret-key shape, mode/provider interlock) and `Warnings` for survivable ones (half a credential pair). `LoadServerFrom` takes an explicit environment map, so the whole contract is unit-testable.

SQL migrations live in `internal/store/postgres/migrations/` as numbered `00NN_name.sql` files, embedded and applied automatically at boot. New schema changes get the next number.

The `Hosted` config flag switches the instance to self-service mode (any forge account may sign in and register workspaces); the default private mode restricts sign-in to members of tracked workspaces.

## Gotchas

- Repo slugs contain a slash (`workspace/repo`), so routes use the `{slug...}` wildcard, not `{slug}`. A single-segment pattern still passes `httptest` handler tests but 404s on the live mux — keep new slug routes on `{slug...}` (see the comment near the route table in `internal/server/server.go`).
- Every upload re-reads and re-merges all parts for the commit under a lock — see `maxPartsPerCommit` in `internal/server/upload.go` before changing the merge path.

## Docs

User-facing docs are in `docs/`, organized user-first: getting-started, per-CI upload recipes (github-actions, gitlab-ci, bitbucket-pipelines, ci-other) plus languages, feature pages (pull-requests, connecting, coverage-gate, parts, coverage-changed), reference (cli, api), and a self-hosting section (self-hosting, sign-in, forge-connections for the operator side of connections, configuration, development). User pages never mention environment variables or deployment; that material stays in the self-hosting section. Update the relevant page when changing user-visible behavior. `docs/sign-in.md` and `docs/configuration.md` keep their filenames (linked from the app's layout template and `TestConfigurationDocIsInSync` respectively), and the pinned-CLI-version snippets live in gitlab-ci.md and ci-other.md (`TestPinnedCLIVersionIsInSync` names the files).

Those same files are the docs site at docs.gocov.dev: `zensical.toml` at the root points at `docs/`, `overrides/` carries the one theme tweak plus a copy of the app's mark (`assets/gocov*.svg` — keep it in step with `internal/server/static/favicon.svg`) and the `_headers` file Cloudflare parses, and CI builds them with `zensical build --strict`, so a link to a page that no longer exists fails the build. Cloudflare runs the same build on every push to `main` and uploads `site/` as static assets per `wrangler.jsonc` — no Worker script, so the file is deploy config only. `docs/` stays plain Markdown — no frontmatter, no site-only files — because it is read on GitHub too, and `docs/README.md` is the site's home page.
