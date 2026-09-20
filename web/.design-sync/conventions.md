# Building with gocov's design system

gocov is a coverage-tracking service. Its UI is deliberately minimal: nine colours, three
spacing steps, four type sizes, one radius, **no shadows and no tinted backgrounds**. Status
is carried by a small coloured dot or mark next to text that stays as readable as the rest of
the page. If a screen needs something this system does not have, that is the signal to
simplify the screen.

## Setup

Wrap the tree once in `PreviewProviders` (exported from the bundle). It supplies a router and
a TanStack query client. Seventeen components render a `Link` — `Button`'s sibling
`LinkButton`, `Breadcrumbs`, `Pagination`, `ReposTable`, `AppShell` and the rest — and they
throw outside a router, so without the wrapper those parts of the page render nothing.

Light and dark both work with no extra wiring: tokens follow `prefers-color-scheme`, and
`<html data-theme="dark">` or `data-theme="light"` pins one.

## The styling idiom

**There is no utility-class framework here.** Do not invent Tailwind-style names — nothing
like `bg-surface-1`, `p-4` or `text-sm` exists and they will resolve to nothing. Each
component owns its own CSS, shipped with the bundle; you never restyle a component from
outside. What you write is the *layout glue between* components, and it uses exactly two
vocabularies:

**Layout utilities** (all of them — this is the complete set):

| Class | What it does |
|---|---|
| `row` | flex row, centred, wraps, 8px gap |
| `row row-2` | the same at 16px |
| `stack` | flex column, 16px gap |
| `stack stack-1` / `stack stack-3` | the same at 8px / 32px |
| `spacer` | `flex:1`, pushes what follows to the end of a `row` |
| `muted` `small` `mono` `num` | secondary colour · 12.5px · monospace · right-aligned tabular figures |
| `sr-only` | text only a screen reader gets |
| `hide-sm` | hidden under 720px |

`row-2`, `stack-1` and `stack-3` are **gap modifiers only** — they must be paired with `row`
or `stack`, or nothing lays out.

**Tokens**, for anything the utilities do not cover. Write `var(--token)`, never a literal:

- Colour: `--bg` `--surface` `--border` `--text` `--muted` `--accent`, plus the three semantic
  ones `--good` `--warn` `--bad` (coverage and gate status: good above 75%, warn 50–75%, bad
  below). `--hover` and `--ring` are derived from those; there are no others.
- Spacing: `--space-1` `--space-2` `--space-3` — 8, 16, 32px. Every margin, padding and gap
  is one of these three. There is no 4px and no 24px.
- Type: `--font-sm` `--font-md` `--font-lg` `--font-xl`, families `--sans` and `--mono`.
- Shape and measure: `--radius`, `--control` / `--control-sm` (button and input heights),
  `--page` (the 1040px content column).

Two rules the system enforces on itself and you should keep: **no hex colours or pixel
spacing** outside these tokens, and **icons only from `Icon`** (twenty names, 16px grid,
`currentColor`).

A `<table>` you write yourself is unstyled — every cell rule lives in `Card`. Put tables
inside `<Card><Card.Body flush>`, which is what the real pages do.

## Where the truth is

Read the files rather than trusting this summary: `_ds/<folder>/styles.css` and what it
imports hold every token and component rule, and
`components/<group>/<Name>/<Name>.prompt.md` documents one component with its props and
examples. The groups are `atoms` (22), `molecules` (28), `organisms` (17) and `templates` (2),
smallest to largest — compose from the largest that fits.

## An idiomatic page fragment

```jsx
const { Card, Button, Chip, CoverageFigure, StatRow, StatTile, PageHeader } = window.GocovUI;

<div className="stack stack-3">
  <PageHeader title="acme/api" meta="main · a1b2c3d · 3 hours ago"
              actions={<Button variant="primary">Settings</Button>} />

  <StatRow>
    <StatTile label="Coverage" value={<CoverageFigure value={74} size="md" />} hint="statement-weighted" />
    <StatTile label="Gates passing" value="6/8" />
  </StatRow>

  <Card>
    <Card.Header title="Uploads" actions={<Chip tone="good">Gate passing</Chip>} />
    <Card.Body>Every repository under acme/ uploads with this token.</Card.Body>
    <Card.Footer>
      <Button>Rotate token</Button>
      <span className="muted small">The old token stops working the moment you rotate.</span>
    </Card.Footer>
  </Card>
</div>
```
