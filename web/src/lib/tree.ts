// The files card's directory tree, built client-side from the flat paths the
// API sends. Ported from buildFileTree in internal/server/uploadpage.go:
// directories roll their files' statements up, a chain of single-child
// directories collapses into one row ("internal/server/"), and the rows come
// out directories-first, each side alphabetical.

import type { FileRow } from "./api/types";

export interface TreeStats {
  /** The full path — a directory's is its deepest segment after collapsing. */
  path: string;
  /** What the row shows: a file's base name, a directory's collapsed name. */
  name: string;
  depth: number;
  coveredStmts: number;
  totalStmts: number;
  coverage: number;
  /** Baseline coverage: a file's own, a directory's rollup; null without one. */
  before: number | null;
  changed: boolean;
  sourceChanged: boolean;
  coverageChanged: boolean;
}

export interface TreeDir extends TreeStats {
  kind: "dir";
  children: TreeNode[];
}

export interface TreeFile extends TreeStats {
  kind: "file";
  row: FileRow;
}

export type TreeNode = TreeDir | TreeFile;

/** What the "Changed" filter and the tree's default open state key on. */
export const isChanged = (row: FileRow) => row.source_changed || row.coverage_changed || row.new_file;

/** Byte-ish order, the same way the server sorted its rows. */
const byName = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);

/** A directory while the tree is assembled. */
interface Building {
  name: string;
  path: string;
  dirs: Map<string, Building>;
  files: FileRow[];
}

const newDir = (name: string, path: string): Building => ({ name, path, dirs: new Map(), files: [] });

/**
 * A directory with no files of its own and exactly one subdirectory absorbs
 * it, so "internal" → "server" → files reads as one "internal/server/" row.
 * Bottom-up, so a longer chain collapses in one pass. The root is never
 * collapsed: it is not a row.
 */
function collapse(dir: Building) {
  for (const sub of dir.dirs.values()) collapse(sub);
  if (dir.path === "" || dir.files.length > 0 || dir.dirs.size !== 1) return;
  const [child] = dir.dirs.values();
  if (child === undefined) return;
  dir.name = dir.name + "/" + child.name;
  dir.path = child.path;
  dir.files = child.files;
  dir.dirs = child.dirs;
}

/** The statements a row contributes, and the baseline behind them. */
interface Roll {
  covered: number;
  total: number;
  beforeCovered: number;
  beforeTotal: number;
  changed: boolean;
  sourceChanged: boolean;
  coverageChanged: boolean;
}

const emptyRoll = (): Roll => ({
  covered: 0,
  total: 0,
  beforeCovered: 0,
  beforeTotal: 0,
  changed: false,
  sourceChanged: false,
  coverageChanged: false,
});

function absorb(into: Roll, from: Roll) {
  into.covered += from.covered;
  into.total += from.total;
  into.beforeCovered += from.beforeCovered;
  into.beforeTotal += from.beforeTotal;
  into.changed ||= from.changed;
  into.sourceChanged ||= from.sourceChanged;
  into.coverageChanged ||= from.coverageChanged;
}

/**
 * A file's contribution. The API sends a baseline coverage but not the
 * baseline's own statement counts, so a directory's "before" is weighted by
 * the file's current statements — an approximation of the server's rollup,
 * exact whenever a file's statement count did not move.
 */
function fileRoll(row: FileRow): Roll {
  return {
    covered: row.covered_stmts,
    total: row.total_stmts,
    beforeCovered: row.before === null ? 0 : (row.before / 100) * row.total_stmts,
    beforeTotal: row.before === null ? 0 : row.total_stmts,
    changed: isChanged(row),
    sourceChanged: row.source_changed,
    coverageChanged: row.coverage_changed,
  };
}

const percent = (covered: number, total: number) => (total > 0 ? (covered / total) * 100 : 0);

function nodesOf(dir: Building, depth: number): { nodes: TreeNode[]; roll: Roll } {
  const roll = emptyRoll();
  const nodes: TreeNode[] = [];

  for (const [, sub] of [...dir.dirs.entries()].sort(([a], [b]) => byName(a, b))) {
    const built = nodesOf(sub, depth + 1);
    absorb(roll, built.roll);
    nodes.push({
      kind: "dir",
      path: sub.path,
      name: sub.name,
      depth,
      coveredStmts: built.roll.covered,
      totalStmts: built.roll.total,
      coverage: percent(built.roll.covered, built.roll.total),
      before: built.roll.beforeTotal > 0 ? percent(built.roll.beforeCovered, built.roll.beforeTotal) : null,
      changed: built.roll.changed,
      sourceChanged: built.roll.sourceChanged,
      coverageChanged: built.roll.coverageChanged,
      children: built.nodes,
    });
  }

  const files = [...dir.files].sort((a, b) => byName(baseName(a.path), baseName(b.path)));
  for (const row of files) {
    const one = fileRoll(row);
    absorb(roll, one);
    nodes.push({
      kind: "file",
      path: row.path,
      name: baseName(row.path),
      depth,
      coveredStmts: row.covered_stmts,
      totalStmts: row.total_stmts,
      coverage: row.coverage,
      before: row.before,
      changed: one.changed,
      sourceChanged: row.source_changed,
      coverageChanged: row.coverage_changed,
      row,
    });
  }

  return { nodes, roll };
}

const baseName = (path: string) => path.slice(path.lastIndexOf("/") + 1);

/** The tree behind a flat list of files, top level first. */
export function buildFileTree(files: FileRow[]): TreeNode[] {
  const root = newDir("", "");
  for (const row of files) {
    const parts = row.path.split("/");
    let dir = root;
    let acc = "";
    for (const part of parts.slice(0, -1)) {
      acc = acc === "" ? part : acc + "/" + part;
      let child = dir.dirs.get(part);
      if (child === undefined) {
        child = newDir(part, acc);
        dir.dirs.set(part, child);
      }
      dir = child;
    }
    dir.files.push(row);
  }
  collapse(root);
  return nodesOf(root, 0).nodes;
}

/**
 * The directories open before anyone clicks: the top level, and — where
 * there is a baseline to compare against — every directory holding a changed
 * file, so a commit's own changes are already in view. A directory counts as
 * changed when anything under it did, so the whole chain down to a change
 * opens with it.
 */
export function defaultOpenDirs(nodes: TreeNode[], hasBase: boolean): Set<string> {
  const open = new Set<string>();
  const walk = (list: TreeNode[]) => {
    for (const node of list) {
      if (node.kind !== "dir") continue;
      if (node.depth === 0 || (hasBase && node.changed)) open.add(node.path);
      walk(node.children);
    }
  };
  walk(nodes);
  return open;
}

/**
 * The tree with only the files `keep` accepts. A directory survives when any
 * descendant does; the rollups it shows stay the ones it was built with, so a
 * filtered directory still reads as the whole directory.
 */
export function filterTree(nodes: TreeNode[], keep: (row: FileRow) => boolean): TreeNode[] {
  const out: TreeNode[] = [];
  for (const node of nodes) {
    if (node.kind === "file") {
      if (keep(node.row)) out.push(node);
      continue;
    }
    const children = filterTree(node.children, keep);
    if (children.length > 0) out.push({ ...node, children });
  }
  return out;
}

/** The tree in display order, skipping whatever a closed directory holds. */
export function visibleRows(nodes: TreeNode[], isOpen: (path: string) => boolean): TreeNode[] {
  const out: TreeNode[] = [];
  const walk = (list: TreeNode[]) => {
    for (const node of list) {
      out.push(node);
      if (node.kind === "dir" && isOpen(node.path)) walk(node.children);
    }
  };
  walk(nodes);
  return out;
}
