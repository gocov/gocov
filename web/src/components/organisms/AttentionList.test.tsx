import { screen, within } from "@testing-library/react";
import type { AttentionItem } from "@/lib/api/types";
import { attentionRows } from "@/lib/dashboard";
import { renderPage } from "@/test/render";
import { AttentionList } from "./AttentionList";

const items: AttentionItem[] = [
  { kind: "failing", forge: "github", slug: "acme/api", name: "api", coverage: 41, min_coverage: 60, stale_days: null },
  { kind: "stale", forge: "github", slug: "acme/web", name: "web", coverage: 82.5, min_coverage: null, stale_days: 21 },
  { kind: "no_gate", forge: "github", slug: "acme/tools", name: "tools", coverage: 66, min_coverage: null, stale_days: null },
];

test("nothing to attend to renders nothing", () => {
  const { container } = renderPage(<AttentionList rows={[]} />);
  expect(container).toBeEmptyDOMElement();
});

test("each notice reads as a sentence with the way to act on it", () => {
  renderPage(<AttentionList rows={attentionRows(items)} />);

  const rows = screen.getAllByRole("listitem");
  expect(rows).toHaveLength(3);

  expect(rows[0]).toHaveTextContent("api is failing its coverage gate");
  expect(rows[0]).toHaveTextContent("Coverage 41.0%, below the 60% minimum.");
  expect(within(rows[0]!).getByRole("img", { name: "Failing" })).toBeInTheDocument();
  expect(within(rows[0]!).getByRole("link", { name: "Open repo" })).toHaveAttribute("href", "/repos/github/acme/api");

  expect(rows[1]).toHaveTextContent("No uploads from web in 21 days");
  expect(within(rows[1]!).getByRole("img", { name: "Stale" })).toBeInTheDocument();

  expect(rows[2]).toHaveTextContent("tools has no coverage gate");
  expect(within(rows[2]!).getByRole("img", { name: "No gate" })).toBeInTheDocument();
  expect(within(rows[2]!).getByRole("link", { name: "Set a gate" })).toHaveAttribute(
    "href",
    "/repo-settings/github/acme/tools",
  );
});

test("the order the server sent is the order shown", () => {
  renderPage(<AttentionList rows={attentionRows([items[2]!, items[0]!])} />);
  const rows = screen.getAllByRole("listitem");
  expect(rows[0]).toHaveTextContent("tools");
  expect(rows[1]).toHaveTextContent("api");
});

test("several gate-less repositories are one notice, each name leading to its own settings", () => {
  const more: AttentionItem[] = [
    ...items,
    { kind: "no_gate", forge: "github", slug: "acme/cli", name: "cli", coverage: 70, min_coverage: null, stale_days: null },
    { kind: "no_gate", forge: "github", slug: "acme/docs", name: "docs", coverage: 55, min_coverage: null, stale_days: null },
  ];
  renderPage(<AttentionList rows={attentionRows(more)} />);

  const rows = screen.getAllByRole("listitem");
  // failing + stale keep their own lines; the three without a gate share one.
  expect(rows).toHaveLength(3);
  expect(rows[2]).toHaveTextContent("3 repositories have no coverage gate");
  // A gate is set per repository — the workspace gate only seeds new ones — so
  // every name goes to that repository's settings and there is no single button.
  expect(within(rows[2]!).getByRole("link", { name: "tools" })).toHaveAttribute("href", "/repo-settings/github/acme/tools");
  expect(within(rows[2]!).getByRole("link", { name: "cli" })).toHaveAttribute("href", "/repo-settings/github/acme/cli");
  expect(within(rows[2]!).getByRole("link", { name: "docs" })).toHaveAttribute("href", "/repo-settings/github/acme/docs");
  expect(within(rows[2]!).getAllByRole("link")).toHaveLength(3);
});
