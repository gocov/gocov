import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { DashRepo } from "@/lib/api/types";
import { renderPage } from "@/test/render";
import { ReposTable } from "./ReposTable";

const repo = (over: Partial<DashRepo> & { name: string }): DashRepo => ({
  forge: "github",
  slug: `acme/${over.name}`,
  coverage: 70,
  delta: null,
  gate: "pass",
  stale: false,
  series: [68, 69, 70],
  uploaded_at: "2026-09-18T09:00:00Z",
  ...over,
});

const repos: DashRepo[] = [
  repo({ name: "api", coverage: 41, delta: -2.4, gate: "fail", uploaded_at: "2026-09-05T09:00:00Z" }),
  repo({ name: "web", coverage: 82.5, delta: 0.4, stale: true, uploaded_at: "2026-08-20T09:00:00Z" }),
  repo({ name: "tools", coverage: 66, gate: "none", uploaded_at: "2026-09-16T09:00:00Z" }),
  repo({ name: "docs", coverage: null, gate: "none", series: [], uploaded_at: null }),
];

/** The repository cell of every body row, in the order they are drawn. */
const shownNames = () =>
  screen
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("cell")[0]?.textContent);

test("lowest coverage leads and a repo with no report sinks", () => {
  renderPage(<ReposTable repos={repos} />);
  expect(shownNames()).toEqual(["api", "tools", "web", "docs"]);

  const rows = screen.getAllByRole("row");
  expect(within(rows[1]!).getByRole("link", { name: "api" })).toHaveAttribute("href", "/repos/github/acme/api");
  expect(within(rows[1]!).getByRole("img", { name: "41.0% covered" })).toBeInTheDocument();
  expect(rows[1]).toHaveTextContent("2.4%");
  expect(within(rows[1]!).getByText("Failing")).toBeInTheDocument();

  // Stale is said as well as the gate state; a repo with no gate offers one.
  expect(within(rows[3]!).getByText("Passing")).toBeInTheDocument();
  expect(within(rows[3]!).getByText("Stale")).toBeInTheDocument();
  expect(within(rows[2]!).getByRole("link", { name: "Set a gate" })).toHaveAttribute(
    "href",
    "/repo-settings/github/acme/tools",
  );
  expect(rows[4]).toHaveTextContent("never");
});

test("the filters carry their counts and slice the table", async () => {
  renderPage(<ReposTable repos={repos} />);
  const filters = screen.getByRole("group", { name: "Filter repositories" });

  expect(within(filters).getByRole("button", { name: /^All/ })).toHaveTextContent("4");
  expect(within(filters).getByRole("button", { name: /^Failing/ })).toHaveTextContent("1");
  expect(within(filters).getByRole("button", { name: /^Stale/ })).toHaveTextContent("1");
  expect(within(filters).getByRole("button", { name: /^No gate/ })).toHaveTextContent("2");

  await userEvent.click(within(filters).getByRole("button", { name: /^No gate/ }));
  expect(shownNames()).toEqual(["tools", "docs"]);
  expect(within(filters).getByRole("button", { name: /^No gate/ })).toHaveAttribute("aria-pressed", "true");

  await userEvent.click(within(filters).getByRole("button", { name: /^Failing/ }));
  expect(shownNames()).toEqual(["api"]);
});

test("search narrows to matching names, and says so when nothing matches", async () => {
  renderPage(<ReposTable repos={repos} />);
  const search = screen.getByRole("searchbox", { name: "Search repositories" });

  await userEvent.type(search, "oo");
  expect(shownNames()).toEqual(["tools"]);

  await userEvent.clear(search);
  await userEvent.type(search, "zzz");
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  expect(screen.getByText("No repositories match.")).toBeInTheDocument();
});

test("the sort control reorders the rows", async () => {
  renderPage(<ReposTable repos={repos} />);
  const sort = screen.getByRole("combobox", { name: "Sort by" });

  await userEvent.selectOptions(sort, "Sort: biggest drop");
  expect(shownNames()).toEqual(["api", "web", "tools", "docs"]);

  await userEvent.selectOptions(sort, "Sort: recently uploaded");
  expect(shownNames()).toEqual(["tools", "api", "web", "docs"]);

  await userEvent.selectOptions(sort, "Sort: name");
  expect(shownNames()).toEqual(["api", "tools", "web", "docs"]);
});
