import type { TrendPoint } from "./api/types";
import { trendGeometry } from "./trend";

const point = (at: string, coverage: number, extra: Partial<TrendPoint> = {}): TrendPoint => ({
  upload_id: 1,
  sha: "abc123def456789",
  coverage,
  at,
  gate_failed: false,
  ...extra,
});

const series: TrendPoint[] = [
  point("2026-08-01T10:00:00Z", 72, { upload_id: 11 }),
  point("2026-08-03T10:00:00Z", 75.5, { upload_id: 12, gate_failed: true }),
  point("2026-08-05T10:00:00Z", 78, { upload_id: 13 }),
];

test("a single point is no chart", () => {
  expect(trendGeometry([], null)).toBeNull();
  expect(trendGeometry(series.slice(0, 1), null)).toBeNull();
});

test("the series fills the plot, oldest on the left", () => {
  const geo = trendGeometry(series, null);
  expect(geo).not.toBeNull();
  if (!geo) return;

  expect(geo.marks).toHaveLength(3);
  expect(geo.marks[0]?.x).toBe(geo.x0);
  expect(geo.marks[2]?.x).toBe(geo.x1);
  // Higher coverage sits higher on the canvas.
  expect(geo.marks[2]!.y).toBeLessThan(geo.marks[0]!.y);
  for (const mark of geo.marks) {
    expect(mark.y).toBeGreaterThan(0);
    expect(mark.y).toBeLessThan(geo.h);
  }
  expect(geo.path.startsWith(`M${geo.x0} `)).toBe(true);
  expect(geo.path).toContain(" L");
  expect(geo.firstDate).toBe("2026-08-01");
  expect(geo.lastDate).toBe("2026-08-05");
  expect(geo.current.label).toBe("78.0%");
});

test("each mark carries its upload, its gate result and a readable label", () => {
  const geo = trendGeometry(series, null);
  expect(geo?.marks.map((m) => m.uploadId)).toEqual([11, 12, 13]);
  expect(geo?.marks.map((m) => m.gateFailed)).toEqual([false, true, false]);
  expect(geo?.marks[1]?.label).toBe("2026-08-03 · 75.5% · abc123def456");
});

test("the grid labels the series min and max, padded away from the edges", () => {
  const geo = trendGeometry(series, null);
  expect(geo?.grid.map((g) => g.label)).toEqual(["78.0%", "72.0%"]);
  // Padding keeps both gridlines inside the plot.
  expect(geo!.grid[0]!.y).toBeGreaterThan(0);
  expect(geo!.grid[1]!.y).toBeLessThan(geo!.h);
});

test("a constant series still draws, with one gridline", () => {
  const flat = [point("2026-08-01T10:00:00Z", 70), point("2026-08-02T10:00:00Z", 70)];
  const geo = trendGeometry(flat, null);
  expect(geo).not.toBeNull();
  expect(geo?.grid).toHaveLength(1);
  expect(geo?.marks[0]?.y).toBe(geo?.marks[1]?.y);
  expect(Number.isFinite(geo?.marks[0]?.y)).toBe(true);
});

test("a gate minimum below the series widens the range so its line lands on the canvas", () => {
  const geo = trendGeometry(series, 60);
  expect(geo?.threshold?.label).toBe("gate 60%");
  expect(geo!.threshold!.y).toBeGreaterThan(geo!.marks[0]!.y);
  expect(geo!.threshold!.y).toBeLessThanOrEqual(geo!.h);
  // The grid still reads the series, not the gate.
  expect(geo?.grid.map((g) => g.label)).toEqual(["78.0%", "72.0%"]);
});

test("a gate minimum above the series lands on the canvas too, and keeps its precision", () => {
  const geo = trendGeometry(series, 92.5);
  expect(geo?.threshold?.label).toBe("gate 92.5%");
  expect(geo!.threshold!.y).toBeLessThan(geo!.marks[2]!.y);
  expect(geo!.threshold!.y).toBeGreaterThanOrEqual(0);
});

test("the current label drops below the point when the top edge is in the way", () => {
  const rising = [point("2026-08-01T10:00:00Z", 10), point("2026-08-02T10:00:00Z", 99.9)];
  const geo = trendGeometry(rising, null);
  expect(geo!.current.y).toBeGreaterThan(geo!.marks[1]!.y);
});

test("the current label sits above the point when there is room", () => {
  const geo = trendGeometry(series, null);
  expect(geo!.current.y).toBeLessThan(geo!.marks[2]!.y);
});

test("the current-value label clears a peak just before the last point", () => {
  // The second-to-last upload is the high point; the label is right-aligned
  // at the last point and would otherwise be drawn across that marker.
  // Dense, like a real branch: 45 uploads put neighbours ~17 units apart.
  const series = [...Array.from({ length: 43 }, (_, i) => 70 + (i % 3) * 0.5), 84, 82];
  const pts = series.map((coverage, i) => ({
    upload_id: i + 1,
    sha: `sha${i}`,
    coverage,
    at: new Date(Date.UTC(2026, 7, 1 + i)).toISOString(),
    gate_failed: false,
  }));
  const g = trendGeometry(pts, null);
  expect(g).not.toBeNull();
  const marks = g!.marks;
  const peak = marks[marks.length - 2]!;
  const last = marks[marks.length - 1]!;
  expect(peak.y).toBeLessThan(last.y); // higher on the canvas
  // Either above the peak, or — when that leaves the canvas — below them both.
  const clear = g!.current.y <= peak.y - 9 || g!.current.y >= last.y + 18;
  expect(clear).toBe(true);
});
