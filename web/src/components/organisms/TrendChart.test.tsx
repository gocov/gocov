import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { TrendPoint } from "@/lib/api/types";
import { TrendChart } from "./TrendChart";

const points: TrendPoint[] = [
  { upload_id: 11, sha: "aaaa1111bbbb2222", coverage: 72, at: "2026-08-01T10:00:00Z", gate_failed: false },
  { upload_id: 12, sha: "cccc3333dddd4444", coverage: 68.5, at: "2026-08-03T10:00:00Z", gate_failed: true },
  { upload_id: 13, sha: "eeee5555ffff6666", coverage: 82.3, at: "2026-08-05T10:00:00Z", gate_failed: false },
];

const draw = (minCoverage: number | null = null, series = points) =>
  render(
    <MemoryRouter>
      <TrendChart points={series} branch="main" minCoverage={minCoverage} />
    </MemoryRouter>,
  );

test("the chart names the branch and its latest figure", () => {
  draw();
  expect(screen.getByRole("img", { name: "Coverage trend on main, latest 82.3%" })).toBeInTheDocument();
});

test("every point links to its upload and says what it is", () => {
  draw();
  const link = screen.getByRole("link", { name: "2026-08-03 · 68.5% · cccc3333dddd" });
  expect(link).toHaveAttribute("href", "/uploads/12");
  expect(screen.getAllByRole("link")).toHaveLength(3);
});

test("the first and last dates and the series range are labelled", () => {
  draw();
  expect(screen.getByText("2026-08-01")).toBeInTheDocument();
  expect(screen.getByText("2026-08-05")).toBeInTheDocument();
  expect(screen.getByText("68.5%")).toBeInTheDocument();
  expect(screen.getAllByText("82.3%").length).toBeGreaterThan(0);
});

test("the gate minimum is drawn and labelled only when there is one", () => {
  const { unmount } = draw();
  expect(screen.queryByText(/^gate /)).not.toBeInTheDocument();
  unmount();

  draw(75);
  expect(screen.getByText("gate 75%")).toBeInTheDocument();
});

test("fewer than two points draws nothing", () => {
  const { container } = draw(null, points.slice(0, 1));
  expect(container).toBeEmptyDOMElement();
});
