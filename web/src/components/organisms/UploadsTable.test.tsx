import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { UploadRow } from "@/lib/api/types";
import { UploadsTable } from "./UploadsTable";

const uploads: UploadRow[] = [
  { id: 412, sha: "a1b2c3d4e5f67890", branch: "main", pr_id: "", coverage: 82.3, gate: "pass", at: new Date().toISOString() },
  { id: 411, sha: "9f2c41d8a7b30000", branch: "fix/upload", pr_id: "128", coverage: 61, gate: "fail", at: new Date().toISOString() },
];

const draw = (rows = uploads, empty?: string) =>
  render(
    <MemoryRouter>
      <UploadsTable uploads={rows} empty={empty} />
    </MemoryRouter>,
  );

test("each upload links to its report and says how the gate went", () => {
  draw();
  expect(screen.getByRole("link", { name: "a1b2c3d4e5f6" })).toHaveAttribute("href", "/uploads/412");
  expect(screen.getByText("Passed")).toBeInTheDocument();
  expect(screen.getByText("Failed")).toBeInTheDocument();
  expect(screen.getAllByText("just now")).toHaveLength(2);
});

test("an upload judged with no gate set says so rather than passing", () => {
  draw([{ ...uploads[0]!, gate: "none" }]);
  expect(screen.getByText("No gate")).toBeInTheDocument();
  expect(screen.queryByText("Passed")).not.toBeInTheDocument();
});

test("a pull request upload carries its number under the sha", () => {
  draw();
  const row = screen.getByRole("link", { name: "9f2c41d8a7b3" }).closest("tr");
  expect(row).not.toBeNull();
  expect(within(row as HTMLElement).getByText("fix/upload")).toBeInTheDocument();
  expect(within(row as HTMLElement).getByText("PR #128")).toBeInTheDocument();
});

test("an empty history says so, in the page's own words", () => {
  draw([], "No uploads on main yet.");
  expect(screen.getByText("No uploads on main yet.")).toBeInTheDocument();
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
});
