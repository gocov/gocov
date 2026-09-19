import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import type { FileRow, FilesView } from "@/lib/api/types";
import { FilesTable } from "./FilesTable";

const file = (path: string, over: Partial<FileRow> = {}): FileRow => ({
  path,
  coverage: 70,
  covered_stmts: 7,
  total_stmts: 10,
  uncovered: "12-18, 40",
  before: null,
  new_file: false,
  newly_uncovered: "",
  source_changed: false,
  coverage_changed: false,
  ...over,
});

const withBase: FilesView = {
  upload_id: 412,
  has_base: true,
  files: [
    file("internal/server/api.go", { before: 62, coverage: 80, source_changed: true, coverage_changed: true, newly_uncovered: "44-46" }),
    file("internal/server/spa.go", { before: 80, coverage: 80 }),
    file("internal/core/pipeline.go", { new_file: true, coverage: 55 }),
    file("main.go", { before: 90, coverage: 90 }),
  ],
};

const noBase: FilesView = {
  upload_id: 7,
  has_base: false,
  files: [file("cmd/gocov/main.go", { covered_stmts: 3, total_stmts: 8 }), file("doc.go")],
};

const draw = (view: FilesView, heading?: string) =>
  render(
    <MemoryRouter>
      <FilesTable view={view} heading={heading} />
    </MemoryRouter>,
  );

const rowNames = () =>
  screen
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("cell")[0]?.textContent?.trim());

test("the tree rolls directories up and files link to their source", () => {
  draw(withBase, "Files on main");
  expect(screen.getByRole("heading", { name: "Files on main" })).toBeInTheDocument();

  // internal holds two collapsed chains, so it stays a directory of its own.
  expect(rowNames()).toEqual(["internal/", "core/", "pipeline.go", "server/", "api.go", "spa.go", "main.go"]);
  expect(screen.getByRole("link", { name: "api.go" })).toHaveAttribute("href", "/uploads/412/files/internal/server/api.go");
});

test("a directory collapses and takes its files with it", async () => {
  draw(withBase);
  const server = screen.getByRole("button", { name: "server/" });
  expect(server).toHaveAttribute("aria-expanded", "true");

  await userEvent.click(server);
  expect(server).toHaveAttribute("aria-expanded", "false");
  expect(screen.queryByRole("link", { name: "api.go" })).not.toBeInTheDocument();

  await userEvent.click(server);
  expect(screen.getByRole("link", { name: "api.go" })).toBeInTheDocument();
});

test("the list view keeps the server's order and dims the directory prefix", async () => {
  draw(withBase);
  await userEvent.click(screen.getByRole("button", { name: /^List/ }));
  expect(rowNames()).toEqual([
    "internal/server/api.go",
    "internal/server/spa.go",
    "internal/core/pipeline.go",
    "main.go",
  ]);
  expect(screen.queryByRole("button", { name: "server/" })).not.toBeInTheDocument();
});

test("the change filter narrows both views and counts what it keeps", async () => {
  draw(withBase);
  const filters = screen.getByRole("group", { name: "Filter files" });
  expect(within(filters).getByRole("button", { name: /^Changed/ })).toHaveTextContent("Changed2");
  expect(within(filters).getByRole("button", { name: /^Source changed/ })).toHaveTextContent("Source changed1");

  // The tree keeps the shape it was built in; only the rows drop out.
  await userEvent.click(within(filters).getByRole("button", { name: /^Source changed/ }));
  expect(rowNames()).toEqual(["internal/", "server/", "api.go"]);
  expect(screen.getByText("1 file")).toBeInTheDocument();

  await userEvent.click(within(filters).getByRole("button", { name: /^Coverage changed/ }));
  expect(rowNames()).toEqual(["internal/", "server/", "api.go"]);
});

test("search keeps the directories on the way to a match", async () => {
  draw(withBase);
  await userEvent.type(screen.getByRole("searchbox", { name: "Search files" }), "pipeline");
  expect(rowNames()).toEqual(["internal/", "core/", "pipeline.go"]);
  expect(screen.getByText("1 file")).toBeInTheDocument();
});

test("a filter that matches nothing says so", async () => {
  draw(withBase);
  await userEvent.type(screen.getByRole("searchbox", { name: "Search files" }), "nothing-here");
  expect(screen.getByText("No files match the selected filter.")).toBeInTheDocument();
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
});

test("with a baseline the table compares, and a new file says it is new", () => {
  draw(withBase);
  expect(screen.getByRole("columnheader", { name: /Before/ })).toBeInTheDocument();
  expect(screen.getByRole("columnheader", { name: "Newly uncovered" })).toBeInTheDocument();
  expect(screen.getByText("new file")).toBeInTheDocument();
  expect(screen.getByText("+18.0%")).toBeInTheDocument();
  expect(screen.getByText("44-46")).toBeInTheDocument();
});

test("without a baseline the table counts statements instead", () => {
  draw(noBase);
  expect(screen.queryByRole("group", { name: "Filter files" })).not.toBeInTheDocument();
  expect(screen.getByRole("columnheader", { name: "Statements" })).toBeInTheDocument();
  expect(screen.getByRole("columnheader", { name: "Uncovered lines" })).toBeInTheDocument();
  // The file, and the directory chain that rolls it up.
  expect(screen.getAllByText("3/8")).toHaveLength(2);
});

test("an upload with no per-file data says so", () => {
  draw({ upload_id: 9, has_base: false, files: [] });
  expect(screen.getByText("No per-file data.")).toBeInTheDocument();
});
