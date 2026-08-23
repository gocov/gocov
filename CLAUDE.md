# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

gocov is a self-hostable coverage-tracking service (Coveralls/Codecov alternative): a single Go binary + Postgres, AGPL-3.0. Sole direct dependency is pgx; everything else is stdlib.

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
- `gocov-server` — API + web UI. Configured entirely via environment variables; the authoritative list is the doc comment at the top of `cmd/gocov-server/main.go` (`DATABASE_URL`, `GOCOV_MODE=hosted`, OAuth keys per forge, `GOCOV_SECRET_KEY`, GitHub App vars, …).
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

`internal/server` is the bulk of the app: HTTP API, SVG badge, and an htmx-based web UI with Go templates and static assets embedded via `go:embed`. `upload.go` is the core flow: authenticate by workspace token → parse profile → store normalized report → merge "parts" (multiple uploads per commit) into a per-commit merged report → evaluate the coverage gate → push status/PR comment/check run to the forge. `internal/diffcov` computes diff coverage against forge-fetched diffs. `internal/auth` handles OAuth sign-in per forge; `internal/secretbox` encrypts stored grant refresh tokens (requires `GOCOV_SECRET_KEY`).

SQL migrations live in `internal/store/postgres/migrations/` as numbered `00NN_name.sql` files, embedded and applied automatically at boot. New schema changes get the next number.

The `Hosted` config flag switches the instance to self-service mode (any forge account may sign in and register workspaces); the default private mode restricts sign-in to members of tracked workspaces.

## Gotchas

- Repo slugs contain a slash (`workspace/repo`), so routes use the `{slug...}` wildcard, not `{slug}`. A single-segment pattern still passes `httptest` handler tests but 404s on the live mux — keep new slug routes on `{slug...}` (see the comment near the route table in `internal/server/server.go`).
- Every upload re-reads and re-merges all parts for the commit under a lock — see `maxPartsPerCommit` in `internal/server/upload.go` before changing the merge path.

## Docs

User-facing docs are in `docs/` (getting-started, sign-in/OAuth setup, forge connections, CI upload, coverage gate, parts, API/badge, configuration, development). Update the relevant page when changing user-visible behavior.
