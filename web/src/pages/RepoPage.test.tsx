import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RepoPage as RepoPageData } from "@/lib/api/types";
import { mockApi, renderPage } from "@/test/render";
import RepoPage from "./RepoPage";

const now = new Date().toISOString();

const repo = (over: Partial<RepoPageData> = {}): RepoPageData => ({
  repo: {
    forge: "github",
    slug: "acme/api",
    default_branch: "main",
    gate: { min_coverage: 80, min_diff_coverage: null, max_coverage_drop: null },
    badge_url: "/badge/github/acme/api",
    badge_markdown: "![coverage](https://app.gocov.dev/badge/github/acme/api)",
    can_settings: true,
  },
  branches: ["main", "fix/upload"],
  branch: "",
  trend_branch: "main",
  summary: {
    verdict: {
      state: "pass",
      coverage: 82.3,
      delta: 1.4,
      reason: "Total coverage 82.3% is at or above the minimum of 80%.",
      base: { upload_id: 410, sha: "0000aaaa1111bbbb", coverage: 80.9 },
    },
    commit: { upload_id: 412, sha: "a1b2c3d4e5f67890", at: now, branch: "main", pr_id: "", is_default: true },
    covered_stmts: 11254,
    total_stmts: 12480,
    last_upload: { at: now, ci_label: "GitHub Actions" },
  },
  trend: [
    { upload_id: 410, sha: "0000aaaa1111bbbb", coverage: 80.9, at: "2026-08-01T10:00:00Z", gate_failed: false },
    { upload_id: 411, sha: "2222cccc3333dddd", coverage: 78, at: "2026-08-03T10:00:00Z", gate_failed: true },
    { upload_id: 412, sha: "a1b2c3d4e5f67890", coverage: 82.3, at: "2026-08-05T10:00:00Z", gate_failed: false },
  ],
  files: {
    upload_id: 412,
    has_base: true,
    files: [
      {
        path: "internal/server/api.go",
        coverage: 80,
        covered_stmts: 8,
        total_stmts: 10,
        uncovered: "12-18",
        before: 62,
        before_covered_stmts: 31,
        before_total_stmts: 50,
        new_file: false,
        newly_uncovered: "",
        source_changed: true,
        coverage_changed: true,
      },
    ],
  },
  uploads: [
    { id: 412, sha: "a1b2c3d4e5f67890", branch: "main", pr_id: "", coverage: 82.3, gate_failed: false, at: now },
  ],
  page: 0,
  has_older: true,
  ...over,
});

const show = (data: RepoPageData, path = "/repos/github/acme/api") => {
  const fetchMock = mockApi({ "GET /repos/github/acme/api": data });
  renderPage(<RepoPage />, { route: "repos/:forge/*", path });
  return fetchMock;
};

const urls = (fetchMock: ReturnType<typeof mockApi>) => fetchMock.mock.calls.map(([input]) => String(input));

test("the page leads with the repo, its badge and its verdict", async () => {
  show(repo());

  expect(await screen.findByRole("heading", { level: 1, name: "acme/api" })).toBeInTheDocument();
  expect(document.title).toBe("acme/api code coverage — gocov");
  expect(screen.getByText(/default branch/)).toHaveTextContent("default branch main · 12,480 statements");
  expect(screen.getByRole("img", { name: "coverage badge" })).toHaveAttribute("src", "/badge/github/acme/api");
  expect(screen.getByRole("textbox", { name: "Badge markdown" })).toHaveValue(
    "![coverage](https://app.gocov.dev/badge/github/acme/api)",
  );
  expect(screen.getByRole("link", { name: "Settings" })).toHaveAttribute("href", "/repo-settings/github/acme/api");

  expect(screen.getByText("Gate passing")).toBeInTheDocument();
  expect(screen.getAllByText("82.3%").length).toBeGreaterThan(0);
  // The verdict's commit, and the same upload in the history below.
  expect(screen.getAllByRole("link", { name: "a1b2c3d4e5f6" })).toHaveLength(2);
  expect(screen.getAllByRole("link", { name: "a1b2c3d4e5f6" })[0]).toHaveAttribute("href", "/uploads/412");
  expect(screen.getByText("1,226")).toBeInTheDocument();
  expect(screen.getByText("total ≥ 80%")).toBeInTheDocument();
});

test("the trend, the files and the uploads all name the branch they describe", async () => {
  show(repo());

  expect(await screen.findByRole("heading", { name: "Coverage over time" })).toBeInTheDocument();
  expect(screen.getByRole("img", { name: "Coverage trend on main, latest 82.3%" })).toBeInTheDocument();
  expect(screen.getByText(/Red points failed the gate/)).toHaveTextContent("the current minimum, 80%.");
  expect(screen.getByRole("heading", { name: "Files on main" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "Uploads" })).toBeInTheDocument();
  expect(screen.getByText("All branches, newest first")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Older" })).toHaveAttribute("href", "/repos/github/acme/api?page=1");
});

test("a repo with no reports yet shows neither summary nor trend", async () => {
  show(repo({ summary: null, trend: [], files: null, uploads: [], has_older: false }));

  expect(await screen.findByText("No uploads yet.")).toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Coverage over time" })).not.toBeInTheDocument();
  expect(screen.queryByText("Gate passing")).not.toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Older" })).not.toBeInTheDocument();
  expect(screen.getByText(/default branch/)).toHaveTextContent("default branch main");
});

test("a reader with no settings access reads the report without the settings link", async () => {
  show(repo({ repo: { ...repo().repo, can_settings: false } }));

  await screen.findByRole("heading", { level: 1, name: "acme/api" });
  expect(screen.queryByRole("link", { name: "Settings" })).not.toBeInTheDocument();
});

test("choosing a branch asks the API for that branch and starts again at page one", async () => {
  const fetchMock = show(repo({ branch: "" }), "/repos/github/acme/api?page=2");
  await screen.findByRole("heading", { level: 1, name: "acme/api" });
  expect(urls(fetchMock).some((url) => url.includes("page=2"))).toBe(true);

  await userEvent.selectOptions(screen.getByRole("combobox", { name: "Branch" }), "fix/upload");
  expect(urls(fetchMock).some((url) => url.includes("branch=fix%2Fupload") && !url.includes("page="))).toBe(true);
});

test("paging keeps the branch in the link", async () => {
  show(repo({ branch: "main" }), "/repos/github/acme/api?branch=main&page=1");

  const pagination = await screen.findByRole("navigation", { name: "Pagination" });
  expect(within(pagination).getByRole("link", { name: "Newer" })).toHaveAttribute(
    "href",
    "/repos/github/acme/api?branch=main",
  );
  expect(within(pagination).getByRole("link", { name: "Older" })).toHaveAttribute(
    "href",
    "/repos/github/acme/api?branch=main&page=2",
  );
  expect(screen.getByText(/newest first/)).toHaveTextContent("On main, newest first");
});

test("a repo the viewer cannot see is a 404, not an error", async () => {
  mockApi({ "GET /repos/github/acme/api": { status: 404, error: "not found" } });
  renderPage(<RepoPage />, { route: "repos/:forge/*", path: "/repos/github/acme/api" });
  expect(await screen.findByText(/couldn’t find that page/)).toBeInTheDocument();
});
