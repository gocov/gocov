import { screen } from "@testing-library/react";
import type { SourceLine, SourcePage as SourceData } from "@/lib/api/types";
import { mockApi, renderPage } from "@/test/render";
import SourcePage from "./SourcePage";

const route = "uploads/:id/files/*";
const path = "/uploads/412/files/internal/server/upload.go";
const key = "GET /uploads/412/files/internal/server/upload.go";

const line = (no: number, hits: number | null, text: string, newMiss = false): SourceLine => ({
  no,
  text,
  hits,
  new_miss: newMiss,
});

const source: SourceData = {
  repo: { forge: "github", slug: "acme/api" },
  upload: { id: 412, sha: "a1b2c3d4e5f6a7b8" },
  file: { path: "internal/server/upload.go", coverage: 62.5, covered_stmts: 25, total_stmts: 40 },
  delta: -1.4,
  unavailable: "",
  uncovered: "12-13, 19",
  lines: [
    line(1, 3, "package server"),
    line(2, null, ""),
    line(3, 0, "func fail() {}", true),
    line(4, 2, "func ok() {}"),
  ],
};

test("names the file, its statements and the commit", async () => {
  mockApi({ [key]: source });
  renderPage(<SourcePage />, { route, path });

  expect(await screen.findByRole("heading", { level: 1 })).toHaveTextContent("internal/server/upload.go");
  expect(document.title).toBe("internal/server/upload.go — gocov");
  expect(screen.getByText("25 of 40 statements covered")).toBeInTheDocument();
  expect(screen.getByText("−1.4%")).toBeInTheDocument();
  expect(screen.getByText("62.5%")).toBeInTheDocument();
  expect(screen.getByText("File coverage")).toBeInTheDocument();
  expect(screen.getByText("1 line newly uncovered")).toBeInTheDocument();
  // The short sha links to the upload from the trail and from the meta line.
  const commit = screen.getAllByRole("link", { name: "a1b2c3d4e5f6" });
  expect(commit).toHaveLength(2);
  for (const link of commit) expect(link).toHaveAttribute("href", "/uploads/412");
});

test("the trail leads back to the repository and the upload", async () => {
  mockApi({ [key]: source });
  renderPage(<SourcePage />, { route, path });

  await screen.findByRole("navigation", { name: "Breadcrumb" });
  expect(screen.getByRole("link", { name: "Repositories" })).toHaveAttribute("href", "/");
  expect(screen.getByRole("link", { name: "acme/api" })).toHaveAttribute("href", "/repos/github/acme/api");
});

test("shows the source with its rail and legend", async () => {
  mockApi({ [key]: source });
  renderPage(<SourcePage />, { route, path });

  expect(await screen.findByText("package server")).toBeInTheDocument();
  expect(screen.getByText("1 uncovered line in 1 block")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Lines 3–3 · 1 uncovered" })).toBeInTheDocument();
  expect(screen.getByText("The rail maps all 4 lines of the file.")).toBeInTheDocument();
  expect(screen.getByText("Newly uncovered by this commit")).toBeInTheDocument();
});

test("a fully covered file says so instead of offering jumps", async () => {
  mockApi({
    [key]: {
      ...source,
      delta: null,
      uncovered: "",
      file: { ...source.file, coverage: 100, covered_stmts: 40, total_stmts: 40 },
      lines: [line(1, 3, "package server"), line(2, 1, "func ok() {}")],
    } satisfies SourceData,
  });
  renderPage(<SourcePage />, { route, path });

  expect(await screen.findByText("Every statement in this file is covered.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Next miss" })).toBeDisabled();
  expect(screen.queryByText(/newly uncovered/)).not.toBeInTheDocument();
});

test("an unavailable source falls back to the uncovered ranges", async () => {
  mockApi({
    [key]: {
      ...source,
      delta: null,
      unavailable: "internal/server/upload.go was not found at commit a1b2c3d4e5f6a7b8 on github",
      lines: [],
    } satisfies SourceData,
  });
  renderPage(<SourcePage />, { route, path });

  expect(await screen.findByText("Source is unavailable:")).toBeInTheDocument();
  expect(screen.getByText(/was not found at commit/)).toBeInTheDocument();
  expect(screen.getByText("Uncovered lines:")).toBeInTheDocument();
  expect(screen.getByText("12-13")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Next miss" })).not.toBeInTheDocument();
});

test("an unavailable source with nothing uncovered says that instead", async () => {
  mockApi({
    [key]: { ...source, delta: null, unavailable: "the file is too large to display", uncovered: "", lines: [] } satisfies SourceData,
  });
  renderPage(<SourcePage />, { route, path });

  expect(await screen.findByText("Source is unavailable:")).toBeInTheDocument();
  expect(screen.getByText("Every statement in this file is covered.")).toBeInTheDocument();
});

test("a file the viewer may not see is a not-found panel", async () => {
  mockApi({});
  renderPage(<SourcePage />, { route, path });
  expect(await screen.findByText("We couldn’t find that page.")).toBeInTheDocument();
});

test("opened from a merged files card, it asks for the commit's parts merged", async () => {
  const fetch = mockApi({ [key]: source });
  renderPage(<SourcePage />, { route, path: path + "?parts=merged" });

  await screen.findByRole("heading", { level: 1 });
  expect(String(fetch.mock.calls[0]?.[0])).toBe("/api/ui/uploads/412/files/internal/server/upload.go?parts=merged");
});
