import { screen, within } from "@testing-library/react";
import type { AttentionItem } from "@/lib/api/types";
import { attentionRows } from "@/lib/dashboard";
import { renderPage } from "@/test/render";
import { AttentionList } from "./AttentionList";

const items: AttentionItem[] = [
  { kind: "failing", forge: "github", slug: "acme/api", name: "api", coverage: 41, min_coverage: 60, stale_days: null },
  { kind: "stale", forge: "github", slug: "acme/web", name: "web", coverage: 82.5, min_coverage: null, stale_days: 21 },
];

test("nothing to attend to renders nothing", () => {
  const { container } = renderPage(<AttentionList rows={[]} />);
  expect(container).toBeEmptyDOMElement();
});

test("each notice reads as a sentence with the way to act on it", () => {
  renderPage(<AttentionList rows={attentionRows(items)} />);

  const rows = screen.getAllByRole("listitem");
  expect(rows).toHaveLength(2);

  expect(rows[0]).toHaveTextContent("api is failing its coverage gate");
  expect(rows[0]).toHaveTextContent("Coverage 41.0%, below the 60% minimum.");
  expect(within(rows[0]!).getByRole("img", { name: "Failing" })).toBeInTheDocument();
  expect(within(rows[0]!).getByRole("link", { name: "Open repo" })).toHaveAttribute("href", "/repos/github/acme/api");

  expect(rows[1]).toHaveTextContent("No uploads from web in 21 days");
  expect(within(rows[1]!).getByRole("img", { name: "Stale" })).toBeInTheDocument();

});

test("the order the server sent is the order shown", () => {
  renderPage(<AttentionList rows={attentionRows([items[1]!, items[0]!])} />);
  const rows = screen.getAllByRole("listitem");
  expect(rows[0]).toHaveTextContent("web");
  expect(rows[1]).toHaveTextContent("api");
});
