# design-sync notes — gocov web

Repo-specific gotchas for `/design-sync`. Read this before re-syncing.

## Shape and why the config looks the way it does

- Shape is **package**, not storybook — there is no Storybook in this repo and there never
  was. The component list comes from the emitted `.d.ts` tree, and preview cards are
  hand-authored under `.design-sync/previews/`.
- `web/` is an **application**, not a published library: `package.json` has no `main`,
  `module`, `exports` or `types`, and `npm run build` produces a Vite app bundle (into
  `../internal/webui/dist`), not a library entry. Two generated artifacts bridge that gap,
  both gitignored:
  - **`web/.design-sync/entry.ts`** (committed) — the design-system barrel the converter
    bundles. Always pass it as `--entry ./.design-sync/entry.ts`. It also imports
    `src/styles/base.css`, which is what puts the tokens, the layout primitives and every
    component's own stylesheet into `_ds_bundle.css`. Do **not** set `cfg.cssEntry`: that
    appends a stylesheet verbatim into `_ds_bundle.css`, and `base.css` starts with
    `@import "./tokens.css"`, which is invalid mid-file and would not resolve at the bundle
    root either.
  - **`web/dist/` + `web/index.d.ts`** (generated, gitignored) — the declaration tree the
    converter reads to derive each `<Name>Props`. Produced by `cfg.buildCmd`
    (`npx tsc -p .design-sync/tsconfig.dts.json`); `web/index.d.ts` is the type entry that
    points at it, because the converter looks for `<pkg>/index.d.ts` when `package.json`
    declares no `types`. **Re-create both before every re-sync** — without them the run
    falls back to synthesizing an entry from `src/`, which would `export *` from
    `src/main.tsx` and execute `createRoot()` at bundle load.

## Gotchas hit and fixed

- **Bare directory imports break the alias resolver.** The converter's esbuild
  tsconfig-paths plugin tries the bare resolved path before `/index`, and `existsSync` is
  true for a directory — so `@/components/atoms` resolved to the directory and the build
  died with `Cannot read file "src/components/atoms": is a directory`. Fixed without a lib
  fork by pointing `cfg.tsconfig` at **`.design-sync/tsconfig.paths.json`**, a resolver-only
  file that lists the four barrel directories explicitly before the `@/*` wildcard. If a new
  barrel directory starts being imported bare, add it there.
- **Six components share a file with a sibling** and the fuzzy src-path finder missed them,
  so they landed in a `general` group: `LinkButton` (in `atoms/Button.tsx`), `ErrorState`,
  `NotFoundState`, `Skeleton` (all in `molecules/QueryBoundary.tsx`), `KeyValue` (in
  `molecules/KeyValueList.tsx`) and `StatTile` (in `molecules/StatRow.tsx`). `cfg.componentSrcMap`
  pins each one, which is what puts them back in `atoms`/`molecules`. A new component added
  to an existing file needs the same pin.
- **Almost everything needs context.** Twenty components import `Link` from react-router and
  two read a TanStack query client, so `cfg.provider` points at `PreviewProviders`
  (`.design-sync/preview-providers.tsx`, exported through `entry.ts`): a `MemoryRouter` — not
  a browser one, which would navigate the card's iframe — around a `QueryClientProvider` with
  retries off.
- **The emitted `.d.ts` was dropping `| null` from every prop union.** The extractor runs the
  TypeScript checker with `strict: false`, which turns `strictNullChecks` off, and the checker
  then collapses `number | null` to `number`. This DS uses null as a real state in a dozen
  contracts — no baseline (`Delta`), no report yet (`CoverageFigure`), a gate that is off
  (`GateRow`), a new file (`BeforeAfter`), a decorative spinner (`Spinner.label`) — so the
  contract the design agent reads was saying the opposite of the truth. Fixed by forking
  `.design-sync/overrides/dts.mjs` with `strictNullChecks: true`, declared in
  `cfg.libOverrides`. The fork needs `.design-sync/node_modules` (a symlink to
  `../.ds-sync/node_modules`, gitignored) so its bare `ts-morph` import resolves —
  **re-create that symlink on a fresh clone**. One limitation the fork does not fix:
  generics still flatten (`QueryBoundary.d.ts` emits `query: unknown` for an unbound `T`).
  On a re-sync, diff the fork against the shipped `lib/dts.mjs` and merge upstream changes.

## Authoring previews for this DS — what the first campaign learned

- **`src/pages/ComponentsPage.tsx` is the composition source, but not verbatim.** It uses
  page-local classes (`Components__dotted`, `Components__icons`) that do not exist inside a
  card, so a literal port silently loses that styling. Its Sparkline "flat" specimen is also
  not flat: `trend()` in `src/lib/format.ts` keys off last-minus-first with a ±0.05
  threshold, so `[70, 70.2, 69.9, 70, 70.1]` draws in the up-green.
- **A bare `<table>` is unstyled.** `base.css` gives `table` only `border-collapse` and
  `width:100%`; every `th`/`td` rule lives in `Card.css` under `.Card__body--flush`. Any cell
  with a table must wrap it in `<Card><Card.Header/><Card.Body flush>`. That is deliberate —
  there is no global table skin — so it is an authoring rule, not a bug.
- **`SaveFooter` has no CSS of its own** (its layout *is* `Card.Footer`'s), so it only reads
  correctly inside a card. Its `busy && saving` vs `busy && !saving` split — the second is
  the *other* card on a page while one is saving — only shows when two cards are previewed
  together.
- **Gap modifiers need their base class**: `.row-2`/`.stack-1`/`.stack-3` only set a gap.
  `class="row-2"` alone is a gap on a non-flex box — it renders, it just looks broken.
- **Icon-ish atoms need visible captions.** `Icon`'s and `Avatar`'s `label` is an accessible
  name only, so a row of bare glyphs reads as an empty card; a `mono muted` caption beside
  each specimen both fills the card and names the axis.
- **A `TextInput` inside a hand-composed `Toolbar` swallows the row** — `.TextInput` is
  `width:100%` and `Toolbar__side` wraps, so a search field plus any second control forces a
  wrap. The real pages fix this with a fixed-width wrapper (`.ReposTable__search`), which a
  preview cannot add without raw pixels; give a Toolbar cell either the search field or the
  other controls.
- **Never hard-code a timestamp in a preview — derive it from `Date.now()`.** Two clocks are
  in play: `package-capture.mjs` pins the page to `2024-05-15T12:00:00Z` so grading sheets are
  deterministic, while the uploaded card renders live in claude.ai/design at the real time. A
  literal is therefore wrong on one side or the other. `timeAgo()` measures elapsed ms, so
  `const ago = (h) => new Date(Date.now() - h * 3_600_000).toISOString()` reads correctly under
  both. Keep offsets under 24h or well over 48h — ~30h back renders "yesterday" beside a
  genuinely two-day-old row. Affects `ProvenanceCard`, `ReposTable`, `UploadsTable`.
- Uncontrolled beats controlled in a cell (`defaultValue`, fixed `checked` + no-op
  `onChange`), but hooks do work — `import { useState } from "react"` resolves through the
  bundle's React shim — and types are stripped, so a settled TanStack result can be stood up
  by hand where a component needs one.

## Card geometry — what the capture can and cannot show

- **Width is not a problem; height is.** The capture loads each cell on its own as
  `?story=<label>`, which replaces the grid with a single full-width render at 900×700, so
  every cell is ~852px wide however many exports a component has — wide components need no
  `cardMode` override. But the shot is `fullPage: false`, so a cell taller than roughly 650px
  is **cut off, not scrolled**, and the review sheet's `max-height` scaling hides it. The tell
  is a card whose bottom border has vanished. Trim the composition, or give the component a
  `viewport` override.
- **`[GRID_OVERFLOW]` catches overflow, not squish — look at the cards yourself.** The card
  grid is `repeat(auto-fit, minmax(320px, 1fr))`, so three stories side by side get ~290px
  each. A component with an internal two-column layout reflows *within* that, which the
  detector reads as fitting, while the render is unusable: `VerdictCard` wrapped its body text
  one word per line with the branch label overlapping the percentage, and nothing flagged it.
  After a clean validate, page through `_screenshots/<group>__<Name>.png` (the full-card
  renders, not the contact sheets) and give `cardMode: "column"` to anything whose story is a
  full-width composition.
- Thirteen components carry `cfg.overrides`. `Tooltip` is `column` because a cell was wider
  than its grid cell; `ConfirmDialog` is `single` because an open native `<dialog>` positions
  outside its cell; `SetupChecklist` is `column` plus a `900x1000` viewport because its
  canonical state — header, three steps and the whole embedded `SnippetPanel` — runs about
  30px past the default and every available lever makes it taller, not shorter. The other ten
  (`AppShell`, `SettingsLayout`, `VerdictCard`, `ProvenanceCard`, `StatRow`, `FilesTable`,
  `ReposTable`, `UploadsTable`, `SnippetPanel`, `SourceViewer`, `SegmentedControl`) are
  `column` for the squish above — tables losing columns, snippets truncated mid-flag, source
  lines cut mid-token.
- Changing `cfg.overrides` needs a full `package-build.mjs` (a scoped `preview-rebuild` after
  one fails `[CONFIG_STALE]`), but it is presentation-only: grades carry forward.

## Organisms: what made their previews renderable

- `AppShell` reads `/api/ui/session`, so its preview stubs `window.fetch` from a module-level
  variable each cell sets while rendering — sound only because the capture loads every cell as
  its own page. Its `<Outlet/>` has no route inside a card, so the cell portals its page body
  into `.AppShell__main`.
- `WorkspaceSwitcher` rendered blank simply because it collapses to a plain title with fewer
  than two workspaces. Its open state is statically renderable: a mount effect clicks the
  trigger, and it stays open because the close handler listens for `mousedown`, which
  `.click()` never fires.
- `SetupChecklist`'s 20-second help state renders too — the pinned capture clock means the
  component's one-second interval never advances past `listeningSince`.
- `FilesTable`, `ReposTable` and `UploadsTable` bring their own `Card` + `Card.Body flush`, so
  the "wrap your table" rule above does not apply to them.
- `AttentionList` takes `AttentionRow[]`, but `attentionRows()` / `attentionCopy()` in
  `src/lib/dashboard.ts` are not bundle exports, so its rows are written out by hand.

## States that cannot be captured statically (deliberate, do not chase)

`Tooltip`'s bubble (visibility:hidden until `:hover`/`:focus-visible` — all three Tooltip
cells show the trigger only), `CopyButton`'s 1200 ms "Copied" confirmation, `Banner`'s
dismissed state, `Checkbox` focus/hover, `CodeBlock`'s keyboard scroll, and `Spinner`/`Toggle`
animation (the sheet freezes mid-rotation, which reads fine). `QueryBoundary`'s 404 branch is
also unreachable from a preview: it keys off `query.error instanceof ApiError`, and `ApiError`
is not exported from `.design-sync/entry.ts` — the visual is covered by the `NotFoundState`
card instead. Its 401 branch renders `null`, which is indistinguishable from a broken cell.

Anything reachable only by clicking is out too, since no prop opens it: `TokenCard`'s rotate
confirmation and its `Rotating…` / new-token / failed states, `ReportingCard`'s disconnect
confirmation, `DangerCard`'s confirm dialog, `ReposTable`'s filter/search/sort, and
`SourceViewer`'s rail jump and expanded fold. `SnippetPanel`'s six-language axis is also not
previewable per cell: the picker seeds from a `localStorage` key shared by every cell on the
card, so all cells show Go and the variation is carried by the `info`-driven axes instead.

## Where the preview compositions come from

`src/pages/ComponentsPage.tsx` is the repo's own component gallery: author-written specimens,
with captions, for every atom and molecule in every state. It is the primary composition
source and the reason previews here are ports rather than inventions. Organisms are not in it
— their compositions come from `src/components/organisms/<Name>.test.tsx` fixtures and from
the real pages under `src/pages/`.

## Known render warns

**None.** The first campaign closed with `package-validate.mjs` printing no warn lines at all:
69/69 previews render cleanly, zero `bad`, zero `thin`, zero `variantsIdentical`, zero
`[GRID_OVERFLOW]`, zero page errors, and no floor cards. Any warn on a future run is therefore
new — look at it rather than assuming it was always there.

## Sequencing lesson from the first campaign

Grade keys include each component's emitted `.d.ts`. The `dts.mjs` fork landed *after* the
first wave had already authored and graded 50 components, so the contract change cleared 51
grades and every one of those sheets had to be re-read. **Settle contract-affecting config —
the dts fork, `componentSrcMap`, `provider` — before authoring anything.** Card-layout
overrides (`cfg.overrides`) have the same effect on the components they name, and they also
need a full `package-build.mjs`: a scoped `preview-rebuild` after changing one fails with
`[CONFIG_STALE]`.

## Re-sync risks

- `ComponentsPage.tsx` is the source previews were ported from. When a component's API
  changes, that page is updated with it, but the preview under `.design-sync/previews/` is
  not — a re-sync re-grades changed components, so look at those sheets rather than assuming
  a port is still current.
- `web/dist/` and `web/index.d.ts` are build inputs that are **not** in git. A fresh clone
  must run `cfg.buildCmd` before the converter, or component discovery silently degrades.
- `.design-sync/tsconfig.paths.json` duplicates the alias list from `web/tsconfig.json`. If
  the `@/*` alias ever changes shape, both files change.
- The declaration build uses the repo's own TypeScript (7.x as of this writing) through
  `npx tsc`, so a toolchain bump changes the emitted `.d.ts` and will re-grade components
  whose contract text moves.
