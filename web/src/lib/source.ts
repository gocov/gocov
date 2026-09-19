// The source view's reading of a file: which lines ran, where the uncovered
// runs are, and which stretches are dull enough to fold away. What a "block"
// and a "fold" mean here came from the Go source page this replaced
// (lineRuns, annotateMisses, foldItems in internal/server/source.go).

import type { SourceLine } from "./api/types";

/** "hit" ran at least once, "miss" never ran, "none" is not a statement. */
export type LineState = "hit" | "miss" | "none";

export const lineState = (line: SourceLine): LineState =>
  line.hits === null ? "none" : line.hits > 0 ? "hit" : "miss";

/** The count beside a line: "12×" when it ran, "0" for a miss, nothing else. */
export const hitsLabel = (hits: number | null): string => (hits === null ? "" : hits > 0 ? `${hits}×` : "0");

/** source.go's foldThreshold: the shortest run of non-miss lines that folds. */
export const FOLD_MIN_LINES = 10;

/** source.go folds a whole run — no line of context is kept around a miss. */
export const FOLD_CONTEXT = 0;

/** source.go's minMissHeight: a one-line run stays a clickable rail target. */
export const MIN_MARKER_HEIGHT = 0.8;

/** A run of consecutive uncovered lines, placed on the rail by percentage. */
export interface MissBlock {
  start: number;
  end: number;
  /** Uncovered lines in the run. */
  lines: number;
  /** Rail offset, percent of the file's height. */
  top: number;
  /** Rail length, percent of the file's height, never below the minimum. */
  height: number;
}

/** A collapsed stretch of lines no miss falls in. */
export interface FoldMarker {
  id: string;
  from: number;
  to: number;
  count: number;
}

export interface LineItem {
  kind: "line";
  line: SourceLine;
  state: LineState;
  /** The fold hiding this line, null when it is always shown. */
  fold: string | null;
  /** Index into `blocks` when this line is uncovered, null otherwise. */
  block: number | null;
}

export interface FoldItem {
  kind: "fold";
  fold: FoldMarker;
}

/** One row of the rendered source: a line, or a bar standing in for a fold. */
export type SourceItem = LineItem | FoldItem;

export interface SourceModel {
  /** Lines in the file — what the rail maps. */
  total: number;
  /** Uncovered lines across every block. */
  missLines: number;
  blocks: MissBlock[];
  items: SourceItem[];
}

/**
 * Reads the lines once: the miss blocks, their rail geometry, and the display
 * rows with long miss-free runs folded away. A file with nothing uncovered
 * folds nothing — folding exists to skip to the misses, and there are none.
 */
export function buildSource(lines: SourceLine[]): SourceModel {
  const total = lines.length;
  if (total === 0) return { total: 0, missLines: 0, blocks: [], items: [] };

  // Maximal runs of misses and of non-misses, exactly like lineRuns.
  interface Run {
    miss: boolean;
    /** Index of the run's first line, its offset into the file. */
    at: number;
    from: SourceLine;
    to: SourceLine;
    lines: { line: SourceLine; state: LineState }[];
  }
  const runs: Run[] = [];
  lines.forEach((line, i) => {
    const state = lineState(line);
    const miss = state === "miss";
    const open = runs.at(-1);
    if (open !== undefined && open.miss === miss) {
      open.lines.push({ line, state });
      open.to = line;
    } else {
      runs.push({ miss, at: i, from: line, to: line, lines: [{ line, state }] });
    }
  });

  const foldable = runs.some((run) => run.miss);
  const blocks: MissBlock[] = [];
  const items: SourceItem[] = [];
  let missLines = 0;
  let folds = 0;

  for (const run of runs) {
    const count = run.lines.length;
    let block: number | null = null;
    let fold: string | null = null;
    if (run.miss) {
      block = blocks.length;
      missLines += count;
      blocks.push({
        start: run.from.no,
        end: run.to.no,
        lines: count,
        top: (run.at / total) * 100,
        height: Math.max((count / total) * 100, MIN_MARKER_HEIGHT),
      });
    } else if (foldable && count >= FOLD_MIN_LINES + 2 * FOLD_CONTEXT) {
      folds++;
      fold = `f${folds}`;
      items.push({ kind: "fold", fold: { id: fold, from: run.from.no, to: run.to.no, count } });
    }
    for (const { line, state } of run.lines) items.push({ kind: "line", line, state, fold, block });
  }
  return { total, missLines, blocks, items };
}
