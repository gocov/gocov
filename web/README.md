# gocov web UI

The single-page app: Vite · React · TypeScript · React Router · TanStack Query · Vitest. Built into `../internal/webui/dist` and embedded in the Go binary; it is the UI, at the canonical URLs (`/`, `/w/…`, `/repos/…`, `/uploads/…`, `/workspace-settings/…`, `/workspace-setup/…`, `/repo-settings/…`, `/login`, `/onboarding`), and Go serves `index.html` as the shell for every one of them.

```sh
npm ci
npm run dev        # http://localhost:5173/ — proxies /api, /oauth, /static … to :8099
npm test           # vitest
npm run build      # typecheck + bundle into ../internal/webui/dist
```

The dev server needs a backend: `GOCOV_PREVIEW_AUTH=1 go run ./cmd/gocov-preview` from the repo root (in-memory store, fake sign-in). Point elsewhere with `GOCOV_BACKEND=http://…`.

## Atomic layers

```
src/components/atoms/       one element, no data fetching, no other components (Icon excepted)
src/components/molecules/   a few atoms that make one reusable unit
src/components/organisms/   a section of a page; takes DTOs as props, never fetches
src/components/templates/   page frames (AppShell, SettingsLayout)
src/pages/                  one per route: fetches with a query from lib/api/queries.ts, composes organisms
src/lib/                    api client + DTO types (the contract with Go), urls, format
```

A layer imports only from layers above it in this list (atoms from nothing, pages from everything). Import siblings through the barrel: `import { Button, Chip } from "@/components/atoms"`. Add every new component to its layer's `index.ts`.

## One component = three files

`Thing.tsx`, `Thing.css`, `Thing.test.tsx`, flat in the layer's folder. Named exports, function components, props typed inline or with an interface in the same file. Look at `atoms/Button.*` and `atoms/CoverageBar.*` before writing one.

CSS is plain, imported by the component, and BEM-named after it: `.Thing`, `.Thing__part`, `.Thing--variant`. No CSS-in-JS, no utility framework, no inline styles except a computed value (a width, a position).

## The design: minimal, on purpose

Everything visual comes from [`src/styles/tokens.css`](src/styles/tokens.css). It is small deliberately.

- **Colour**: `--bg --surface --border --text --muted --accent` plus `--good --warn --bad`. The last three mean coverage/gate status and nothing else — never decoration. No other colour values in component CSS (no hex, no rgb). Need a tint? `color-mix()` from a token, or `--hover` / `--ring`.
- **Status is a dot, not a pill**: see `atoms/Chip`. No tinted backgrounds, no coloured left borders, no shadows, no gradients.
- **Space**: only `--space-1` (8px), `--space-2` (16px), `--space-3` (32px) for every margin, padding and gap. Tight inline gaps inside an atom (icon–label) may use a literal ≤ 6px. Prefer `gap` on a flex/grid parent over margins; `.stack` / `.row` in `base.css` cover most layouts.
- **Type**: `--font-sm --font-md --font-lg --font-xl`, weights 400 / 500 / 600. `--mono` for slugs, SHAs, branches, paths, env names.
- **Shape**: `--radius` everywhere, `--control` for the height of buttons/inputs/selects. Cards are `background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius)`.
- **Icons**: only `atoms/Icon`. No emoji, no glyph characters (✓ ✗ ▲ ▼ →) in the UI. Missing one? Add a 16px-grid stroke path there.
- Light and dark both come from the tokens; never write a colour scheme rule in a component.
- Works at 360px: tables scroll inside their card, secondary columns use `.hide-sm`.
- Accessible: real `<button>`/`<a>`, labels on inputs, `aria-pressed` on toggles and segmented controls, state never by colour alone.

## Data

- The DTOs in [`src/lib/api/types.ts`](src/lib/api/types.ts) are the contract with `internal/server/api*.go`. Do not change them without changing the Go side.
- Pages fetch with `useQuery(xQuery(...))` from `lib/api/queries.ts` and render through `<QueryBoundary query={q}>{(data) => …}</QueryBoundary>` (skeleton / 404 / retryable error). Mutations: `useMutation` + `apiPost`, then `queryClient.setQueryData` or `invalidateQueries`.
- Numbers and ISO timestamps come from the API; words come from `lib/format.ts` (`pct`, `deltaText`, `timeAgo`, `level`, …). Add helpers there, with a test.
- Links inside the SPA use `<Link to={routes.x(...)}>` from `lib/urls.ts`; what the browser leaves the app for (`server.*`: the OAuth start, logout, docs) uses a plain `<a href>`.
- Every page calls `usePageTitle()` from `lib/title.ts`; a title that needs loaded data is passed as `undefined` until it lands. `index.html` carries exactly `<title>gocov</title>` on one line, which the Go server replaces to inject per-page head tags — `src/test/shell.test.ts` guards it.
- Slugs, workspace prefixes (a GitLab group nests: `grp/sub`) and file paths contain slashes: routes end in a splat, read it with `useParams()["*"]`. Nothing rides as a `%2F` segment — a proxy in front of the server may decode or refuse one — so a page or an action is named *before* the workspace (`/workspace-settings/{forge}/{prefix…}`), never after it.

## Tests

Vitest + Testing Library, globals on (`test`, `expect`, `vi`). Query by role and name, not by class. Pages: `mockApi({ "GET /dashboard": fixture })` then `renderPage(<DashboardPage />, { route, path })` from `src/test/render.tsx`; type fixtures with the DTO types so a contract change breaks the test.
