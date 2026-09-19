import { screen } from "@testing-library/react";
import type { UploadPage as UploadPageData } from "@/lib/api/types";
import { mockApi, renderPage } from "@/test/render";
import UploadPage from "./UploadPage";

const now = new Date().toISOString();

const upload = (over: Partial<UploadPageData> = {}): UploadPageData => ({
  repo: { forge: "github", slug: "acme/api" },
  upload: {
    id: 412,
    sha: "a1b2c3d4e5f67890",
    branch: "fix/upload",
    pr_id: "128",
    at: now,
    commit_message: "server: keep the upload page out of search indexes",
    commit_author: "omer",
    tokenless: false,
  },
  verdict: {
    state: "fail",
    coverage: 74,
    delta: -1.1,
    reason: "Diff coverage 48.0% is below the minimum of 70%.",
    base: { upload_id: 410, sha: "0000aaaa1111bbbb", coverage: 75.1 },
  },
  covered_stmts: 9236,
  total_stmts: 12481,
  file_count: 2,
  format: "go",
  diff: { coverage: 48, covered_lines: 12, total_lines: 25, changed_files: 3, unmatched_files: 1 },
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
        newly_uncovered: "44-46",
        source_changed: true,
        coverage_changed: true,
      },
      {
        path: "internal/server/spa.go",
        coverage: 55,
        covered_stmts: 11,
        total_stmts: 20,
        uncovered: "30-40",
        before: null,
        before_covered_stmts: null,
        before_total_stmts: null,
        new_file: true,
        newly_uncovered: "",
        source_changed: true,
        coverage_changed: false,
      },
    ],
  },
  provenance: {
    received_at: now,
    profile_name: "coverage.out",
    profile_size: "184 kB",
    format: "go",
    ci_label: "GitHub Actions #2184",
    ci_run_url: "https://github.com/acme/api/actions/runs/2184",
    uploader: "gocov-action v1.17.0",
    uploader_kind: "action",
    part: "unit",
    parts_note: "3 of 3 parts merged",
    processed: "412 ms",
    ignored: "",
  },
  download_url: "/uploads/412/profile",
  ...over,
});

const show = (data: UploadPageData) => {
  mockApi({ "GET /uploads/412": data });
  renderPage(<UploadPage />, { route: "uploads/:id", path: "/uploads/412" });
};

test("the page leads with the commit and the verdict against its base", async () => {
  show(upload());

  expect(
    await screen.findByRole("heading", { level: 1, name: "server: keep the upload page out of search indexes" }),
  ).toBeInTheDocument();
  expect(document.title).toBe("acme/api @ a1b2c3d4e5f6 — gocov");
  expect(screen.getByText("fix/upload")).toBeInTheDocument();
  expect(screen.getByText("PR #128")).toBeInTheDocument();
  expect(screen.getByText(/by/)).toHaveTextContent("by omer");

  expect(screen.getByText("Gate failing")).toBeInTheDocument();
  expect(screen.getByText("74.0%")).toBeInTheDocument();
  expect(screen.getByText("−1.1%")).toBeInTheDocument();
  expect(screen.getByText(/9,236\/12,481/)).toBeInTheDocument();
  // The report's own count, beside the files card's match count.
  expect(screen.getAllByText(/2 files/)).toHaveLength(2);
  expect(screen.getByText(/go profile/)).toBeInTheDocument();
  expect(screen.getAllByRole("link", { name: "0000aaaa1111" })[0]).toHaveAttribute("href", "/uploads/410");
  expect(screen.getByText(/the last gate-passing upload on this branch/)).toBeInTheDocument();
});

test("the breadcrumb goes back to the repository", async () => {
  show(upload());
  expect(await screen.findByRole("link", { name: "acme/api" })).toHaveAttribute("href", "/repos/github/acme/api");
});

test("a pull request upload reports its diff coverage", async () => {
  show(upload());

  expect(await screen.findByText("Diff coverage")).toBeInTheDocument();
  expect(screen.getByText("48.0%")).toBeInTheDocument();
  expect(screen.getByText("12 of 25 new lines")).toBeInTheDocument();
  expect(screen.getByText("Uncovered lines added")).toBeInTheDocument();
  expect(screen.getByText("13")).toBeInTheDocument();
  expect(screen.getByText("+1 without data")).toBeInTheDocument();
});

test("the files and the provenance of the upload are both there", async () => {
  show(upload());

  expect(await screen.findByRole("heading", { name: "Files" })).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "api.go" })).toHaveAttribute(
    "href",
    "/uploads/412/files/internal/server/api.go",
  );
  expect(screen.getByText("new file")).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "Upload" })).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Download profile" })).toHaveAttribute("href", "/uploads/412/profile");
});

test("a first upload has no base, no diff and no baseline note", async () => {
  show(
    upload({
      upload: { ...upload().upload, pr_id: "", commit_message: "", branch: "main" },
      verdict: { state: "neutral", coverage: 74, delta: null, reason: "No gate is configured.", base: null },
      diff: null,
      files: { upload_id: 412, has_base: false, files: [] },
      download_url: null,
    }),
  );

  expect(await screen.findByRole("heading", { level: 1, name: "a1b2c3d4e5f6" })).toBeInTheDocument();
  expect(screen.getByText("Coverage recorded")).toBeInTheDocument();
  expect(screen.queryByText("Diff coverage")).not.toBeInTheDocument();
  expect(screen.queryByText(/the last gate-passing upload/)).not.toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Download profile" })).not.toBeInTheDocument();
  expect(screen.getByText("No per-file data.")).toBeInTheDocument();
});

test("a fork upload says it was not verified by a token, and why", async () => {
  show(upload({ upload: { ...upload().upload, tokenless: true } }));

  expect(await screen.findByText("unverified contributor upload")).toBeInTheDocument();
  expect(screen.getByRole("tooltip")).toHaveTextContent(/authenticated by verifying the workflow run/);
});
