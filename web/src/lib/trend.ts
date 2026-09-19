// The coverage chart's geometry, ported from newTrendView in
// internal/server/trend.go so the SPA plots the same shape the server-
// rendered page did. Pure: the component only places these coordinates.

import type { TrendPoint } from "./api/types";
import { shortSha } from "./format";

/** How far left of the last point the current-value label can reach, in viewBox units. */
const LABEL_REACH = 64;
const W = 800;
const H = 160;
const PAD_L = 46;
const PAD_R = 14;
const PAD_T = 12;
const PAD_B = 24;

/** One upload's marker: where it sits, where it links, and what it reads as. */
export interface TrendMark {
  x: number;
  y: number;
  uploadId: number;
  gateFailed: boolean;
  /** "2026-08-05 · 82.3% · abc123def456" */
  label: string;
}

/** A labelled horizontal line: the series min and max, or the gate minimum. */
export interface TrendLine {
  y: number;
  label: string;
}

export interface TrendGeometry {
  w: number;
  h: number;
  /** The plot's left and right edge: lines and axis labels hang off these. */
  x0: number;
  x1: number;
  /** The polyline through every point. */
  path: string;
  marks: TrendMark[];
  grid: TrendLine[];
  /** The gate minimum, drawn dashed; null when the repo has no minimum. */
  threshold: TrendLine | null;
  /** The latest value, labelled at the last point. */
  current: { x: number; y: number; label: string };
  firstDate: string;
  lastDate: string;
}

/** SVG coordinates stay at one decimal, so the markup stays compact. */
const round1 = (v: number) => Math.round(v * 10) / 10;

const day = (iso: string) => iso.slice(0, 10);

const pct1 = (v: number) => v.toFixed(1) + "%";

/** Go's %.4g: four significant digits with trailing zeros dropped. */
const sig4 = (v: number) => String(Number(v.toPrecision(4)));

/**
 * The chart for a branch, given its points oldest first (the API already
 * drops PR reports). Fewer than two points returns null — the page then omits
 * the section. A gate minimum widens the plotted range so its line always
 * lands on the canvas, while the grid labels keep reading the series min and
 * max, so the gate reads as a reference and not as a data point.
 */
export function trendGeometry(points: TrendPoint[], minCoverage: number | null): TrendGeometry | null {
  const first = points[0];
  const last = points[points.length - 1];
  if (points.length < 2 || first === undefined || last === undefined) return null;

  // Y auto-scales to the data's range with padding, not 0–100: a 72–78%
  // story must not flatline. Clamped, so the padding never invents an
  // impossible percentage.
  const values = points.map((p) => p.coverage);
  const lo = Math.min(...values);
  const hi = Math.max(...values);
  const pad = Math.max((hi - lo) * 0.1, 0.5);
  let yLo = Math.max(0, lo - pad);
  let yHi = Math.min(100, hi + pad);
  if (minCoverage !== null) {
    yLo = Math.min(yLo, Math.max(0, minCoverage));
    yHi = Math.max(yHi, Math.min(100, minCoverage));
  }
  const span = yHi - yLo || 1;

  const plotW = W - PAD_L - PAD_R;
  const plotH = H - PAD_T - PAD_B;
  const x = (i: number) => round1(PAD_L + (plotW * i) / (points.length - 1));
  const y = (value: number) => round1(PAD_T + (plotH * (yHi - value)) / span);

  const marks: TrendMark[] = points.map((p, i) => ({
    x: x(i),
    y: y(p.coverage),
    uploadId: p.upload_id,
    gateFailed: p.gate_failed,
    label: `${day(p.at)} · ${pct1(p.coverage)} · ${shortSha(p.sha)}`,
  }));
  const path = marks.map((m, i) => `${i === 0 ? "M" : " L"}${m.x} ${m.y}`).join("");

  const grid: TrendLine[] = [{ y: y(hi), label: pct1(hi) }];
  if (lo !== hi) grid.push({ y: y(lo), label: pct1(lo) });

  const lastMark = marks[marks.length - 1] as TrendMark;
  // The label is right-aligned at the last point, so it stretches back over
  // the points just before it: clear the highest of those, not only the last
  // one — a peak on the second-to-last upload used to sit under the text.
  // When that would leave the canvas, it goes below the lowest of them.
  const under = marks.filter((m) => m.x >= lastMark.x - LABEL_REACH);
  const top = Math.min(...under.map((m) => m.y));
  const labelY = top - 9 < 11 ? Math.max(...under.map((m) => m.y)) + 18 : top - 9;

  return {
    w: W,
    h: H,
    x0: PAD_L,
    x1: W - PAD_R,
    path,
    marks,
    grid,
    threshold: minCoverage === null ? null : { y: y(minCoverage), label: `gate ${sig4(minCoverage)}%` },
    current: { x: lastMark.x, y: labelY, label: pct1(last.coverage) },
    firstDate: day(first.at),
    lastDate: day(last.at),
  };
}
