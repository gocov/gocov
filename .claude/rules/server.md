---
paths:
  - "internal/server/**"
---

# internal/server — transport and web UI

`internal/server` is the transport around the pipeline: the HTTP API, the SVG badge, and the web UI, which is the single-page app in `web/` (embedded through `internal/webui`). Every page route serves that app's shell (`spa.go`) — Go decides the status code and the head tags, React draws the body — and the app reads its data from the JSON API in `api*.go` (`/api/ui/`), which is where access is decided. `upload.go` authenticates the token, reads and validates the multipart request, and hands a `core.Submission` to the pipeline, which resolves its own forge client. The connect flows (`githubapp.go`, `grant.go` for the Bitbucket and GitLab OAuth grants, `githubwebhook.go`) are the handlers around those connections — consent redirects, callbacks, disconnect — while the tokens behind them live in `core`.

## File layout

Files are named for what they serve — one screen or concern each (`page`-style files like `repopage.go`/`uploadpage.go`, which hold both the page route's access decision and the UI API handler behind that screen, `oauth.go`/`session.go`/`scope.go` for identity, `workspace.go`/`register.go`/`repo_settings.go` for administration) — and each has a test file of the same name. The exceptions are deliberate:

- `upload_gate_test.go`, `upload_merge_test.go` and `upload_push_test.go` drive the upload endpoint end to end for behaviour whose unit tests live in `internal/core`;
- `public_reports_test.go` drives the anonymous report pages end to end across the handlers whose access decision lives in `scope.go` and `session.go`;
- `bitbucketgrant_test.go`/`gitlabgrant_test.go` drive `grant.go` once per forge, since the handlers are one table-driven set but each forge's fixture, consent and rotation are its own.

## Gotchas

- Repo slugs contain a slash (`workspace/repo`) and GitLab workspace prefixes can (`grp/sub`), so routes use the `{slug...}` / `{prefix...}` wildcard, not a single segment. A single-segment pattern still passes `httptest` handler tests but 404s on the live mux, and an escaped `%2F` segment is a proxy's to decode or refuse. A wildcard must come last, so a page or a verb is named before the tenant: `/workspace-settings/save/{forge}/{prefix...}`, never `/workspaces/{forge}/{prefix}/settings` (see `urls.go` and the route table in `server.go`). The pre-v0.26 addresses (`/?ws=`, `/workspaces/{forge}/{prefix}[/setup]`) answer with a 301.
- Every upload re-reads and re-merges all parts for the commit under a lock — see `maxPartsPerCommit` in `upload.go` before changing the merge path.
- For eyeballing UI changes without Postgres or OAuth: `go run ./cmd/gocov-preview` (`GOCOV_PREVIEW_AUTH=1` adds fake sign-in). The preview server is configured in `.claude/launch.json`; use the browser preview tools rather than curl to check a rendered page. It needs a web build (`cd web && npm run build`) — without one every page serves the built-in stand-in shell instead.
