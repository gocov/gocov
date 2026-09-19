---
paths:
  - "web/**"
  - "internal/webui/**"
  - "internal/server/api*.go"
  - "internal/server/spa*.go"
---

# web/ — the single-page web UI

`web/` is the web UI: a Vite + React + TypeScript single-page app (it replaced the Go-template UI in September 2026). `web/README.md` holds the conventions (atomic layers, one component = three files, the deliberately minimal tokens). Read the README before writing a component.

- The build writes to `internal/webui/dist`, which `internal/webui` embeds; only `.gitkeep` there is committed. Go builds and tests must keep passing without a web build.
- Go serves the app's shell for every page route (`internal/server/spa.go`) and decides the status code and the head tags — per-page `<title>`, the public repo page's description and canonical link, `noindex` on upload and source pages — by replacing the literal `<title>gocov</title>` in `web/index.html`; bundles ride under `/static/app/`. A page route added in `web/src/router.tsx` needs its twin in `routes()` in `server.go`, or a reload 404s.
- What stays a server route is a browser navigation or a machine endpoint: OAuth, `/logout`, the consent start (`/workspaces/{forge}/{prefix}/connect`), `/github/setup`, the raw profile download, badge, upload API. Their redirects land on app routes and carry codes, never prose (`?connect=`, `?error=connect_failed|connect_denied|connect_owners_only`); the app writes the sentences (`web/src/lib/connect.ts`, and each page's code table for `lib/notice.ts`, which drops any value it has no sentence for).
- `web/src/lib/api/types.ts` is the contract with `internal/server/api*.go` (`/api/ui/*`). Change both sides together. Responses are explicit DTOs — never marshal a store row, they carry upload tokens; `TestAPINeverLeaksTokens` guards it. Token values come only from the reveal/rotate POSTs.
- Access decisions stay in Go: a non-member gets 404, not 403, from the page route and from the API alike — nothing may reveal that a repository exists.
- Design: nine colour tokens, three spacing steps, status as a dot, no shadows. No hex colours or pixel spacing in component CSS. Icons only from `atoms/Icon`.
- Session replay covers only `/onboarding` and the workspace setup page — never the dashboard, which names private repositories (`web/src/lib/analytics.ts`); the PostHog project's URL trigger config must list the same patterns.
- Verify UI work against `go run ./cmd/gocov-preview` (it serves the embedded build, so run `npm run build` in `web/` first; or use the `web` launch config for live reload), in the Browser pane, light and dark and at phone width.
