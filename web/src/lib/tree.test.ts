import type { FileRow } from "./api/types";
import { buildFileTree, defaultOpenDirs, filterTree, isChanged, visibleRows, type TreeDir, type TreeNode } from "./tree";

const file = (path: string, covered: number, total: number, extra: Partial<FileRow> = {}): FileRow => ({
  upload_id: 1,
  path,
  coverage: total === 0 ? 0 : (covered / total) * 100,
  covered_stmts: covered,
  total_stmts: total,
  uncovered: "",
  before: null,
  before_covered_stmts: null,
  before_total_stmts: null,
  new_file: false,
  newly_uncovered: "",
  source_changed: false,
  coverage_changed: false,
  ...extra,
});

const dirs = (nodes: TreeNode[]) => nodes.filter((n): n is TreeDir => n.kind === "dir");
const names = (nodes: TreeNode[]) => nodes.map((n) => n.name);

test("files without a directory stay at the top level", () => {
  const tree = buildFileTree([file("main.go", 4, 5), file("doc.go", 1, 1)]);
  expect(names(tree)).toEqual(["doc.go", "main.go"]);
  expect(tree[0]?.kind).toBe("file");
});

test("a directory rolls its files' statements up", () => {
  const tree = buildFileTree([file("internal/a.go", 6, 10), file("internal/b.go", 4, 10)]);
  const [dir] = dirs(tree);
  expect(dir?.name).toBe("internal");
  expect(dir?.coveredStmts).toBe(10);
  expect(dir?.totalStmts).toBe(20);
  expect(dir?.coverage).toBe(50);
  expect(dir?.children).toHaveLength(2);
});

test("the rollup reaches through nested directories", () => {
  const tree = buildFileTree([
    file("internal/server/a.go", 5, 10),
    file("internal/core/b.go", 5, 10),
    file("cmd/gocov/main.go", 1, 10),
  ]);
  expect(names(tree)).toEqual(["cmd/gocov", "internal"]);
  const internal = dirs(tree).find((d) => d.name === "internal");
  expect(internal?.totalStmts).toBe(20);
  expect(internal?.coveredStmts).toBe(10);
  expect(names(internal?.children ?? [])).toEqual(["core", "server"]);
});

test("a chain of single-child directories collapses into one row", () => {
  const tree = buildFileTree([file("internal/server/api.go", 1, 2), file("internal/server/spa.go", 1, 2)]);
  expect(names(tree)).toEqual(["internal/server"]);
  expect(tree[0]?.path).toBe("internal/server");
  expect(tree[0]?.depth).toBe(0);
  expect(names((tree[0] as TreeDir).children)).toEqual(["api.go", "spa.go"]);
});

test("a directory that holds a file of its own is not collapsed away", () => {
  const tree = buildFileTree([file("internal/doc.go", 1, 1), file("internal/server/api.go", 1, 2)]);
  expect(names(tree)).toEqual(["internal"]);
  const internal = tree[0] as TreeDir;
  expect(names(internal.children)).toEqual(["server", "doc.go"]);
  expect(internal.children[0]?.depth).toBe(1);
});

test("directories come before files, each side alphabetical", () => {
  const tree = buildFileTree([file("zz.go", 1, 1), file("aa/one.go", 1, 1), file("ab/two.go", 1, 1), file("ba.go", 1, 1)]);
  expect(names(tree)).toEqual(["aa", "ab", "ba.go", "zz.go"]);
});

/** A file that had `covered` of `total` statements at the baseline. */
const was = (covered: number, total: number): Partial<FileRow> => ({
  before: (covered / total) * 100,
  before_covered_stmts: covered,
  before_total_stmts: total,
});

test("a directory's baseline is rolled up from the baseline's own statement counts", () => {
  const tree = buildFileTree([
    file("internal/a.go", 8, 10, was(5, 10)),
    file("internal/b.go", 5, 10, was(7, 10)),
    file("internal/new.go", 3, 10, { new_file: true }),
  ]);
  const [dir] = dirs(tree);
  // (5 + 7) of 20 baseline statements; the new file had none to add.
  expect(dir?.before).toBeCloseTo(60, 5);
});

test("a file that grew since the baseline weighs what it weighed then", () => {
  const tree = buildFileTree([
    // 1 of 2 statements then, 90 of 100 now: weighting its 50% by today's
    // hundred statements would drag the directory's "before" down to ~55%.
    file("internal/grew.go", 90, 100, was(1, 2)),
    file("internal/same.go", 10, 10, was(10, 10)),
  ]);
  const [dir] = dirs(tree);
  expect(dir?.before).toBeCloseTo((11 / 12) * 100, 5);
});

test("a directory without any baseline below it has none", () => {
  const tree = buildFileTree([file("internal/a.go", 8, 10)]);
  expect(dirs(tree)[0]?.before).toBeNull();
});

test("a change anywhere below flags the whole chain", () => {
  const tree = buildFileTree([
    file("internal/server/api.go", 1, 2, { coverage_changed: true }),
    file("internal/server/spa.go", 1, 2),
    file("cmd/main.go", 1, 2),
  ]);
  const internal = dirs(tree).find((d) => d.name === "internal/server");
  expect(internal?.changed).toBe(true);
  expect(internal?.coverageChanged).toBe(true);
  expect(dirs(tree).find((d) => d.name === "cmd")?.changed).toBe(false);
});

test("a new file counts as changed", () => {
  expect(isChanged(file("a.go", 1, 2, { new_file: true }))).toBe(true);
  expect(isChanged(file("a.go", 1, 2, { source_changed: true }))).toBe(true);
  expect(isChanged(file("a.go", 1, 2))).toBe(false);
});

test("a filter keeps the ancestors of every file it matches", () => {
  const tree = buildFileTree([
    file("internal/server/api.go", 1, 2, { source_changed: true }),
    file("internal/server/spa.go", 1, 2),
    file("cmd/main.go", 1, 2),
  ]);
  const kept = filterTree(tree, (row) => row.source_changed);
  expect(names(kept)).toEqual(["internal/server"]);
  expect(names((kept[0] as TreeDir).children)).toEqual(["api.go"]);
  // The directory keeps the rollup it was built with.
  expect((kept[0] as TreeDir).totalStmts).toBe(4);
});

test("a directory with nothing left drops out entirely", () => {
  const tree = buildFileTree([file("cmd/main.go", 1, 2)]);
  expect(filterTree(tree, () => false)).toEqual([]);
});

test("the top level opens by default, and so does the way to a change", () => {
  const tree = buildFileTree([
    file("internal/server/api.go", 1, 2, { coverage_changed: true }),
    file("cmd/gocov/main.go", 1, 2),
  ]);
  const open = defaultOpenDirs(tree, true);
  expect(open.has("internal/server")).toBe(true);
  expect(open.has("cmd/gocov")).toBe(true);
});

test("without a baseline only the top level opens", () => {
  const tree = buildFileTree([file("internal/a/b/deep.go", 1, 2), file("internal/doc.go", 1, 2)]);
  const open = defaultOpenDirs(tree, false);
  expect(open.has("internal")).toBe(true);
  expect(open.has("internal/a/b")).toBe(false);
});

test("closed directories hide what they hold", () => {
  const tree = buildFileTree([file("internal/server/api.go", 1, 2), file("cmd/main.go", 1, 2)]);
  const all = visibleRows(tree, () => true);
  expect(names(all)).toEqual(["cmd", "main.go", "internal/server", "api.go"]);
  const closed = visibleRows(tree, (path) => path !== "internal/server");
  expect(names(closed)).toEqual(["cmd", "main.go", "internal/server"]);
});
