import type { SourceLine } from "./api/types";
import { FOLD_MIN_LINES, buildSource, hitsLabel, lineState, type FoldItem, type LineItem } from "./source";

/** A file from its hit counts: null = not a statement, 0 = never executed. */
const file = (hits: (number | null)[], newMiss: number[] = []): SourceLine[] =>
  hits.map((h, i) => ({ no: i + 1, text: `line ${i + 1}`, hits: h, new_miss: newMiss.includes(i + 1) }));

const lines = (model: ReturnType<typeof buildSource>) => model.items.filter((i): i is LineItem => i.kind === "line");
const folds = (model: ReturnType<typeof buildSource>) => model.items.filter((i): i is FoldItem => i.kind === "fold");

test("a line's state comes from its hit count", () => {
  expect(lineState({ no: 1, text: "", hits: 3, new_miss: false })).toBe("hit");
  expect(lineState({ no: 1, text: "", hits: 0, new_miss: false })).toBe("miss");
  expect(lineState({ no: 1, text: "", hits: null, new_miss: false })).toBe("none");
});

test("the hit count reads as a multiplier, and a non-statement says nothing", () => {
  expect(hitsLabel(12)).toBe("12×");
  expect(hitsLabel(0)).toBe("0");
  expect(hitsLabel(null)).toBe("");
});

test("miss blocks are runs of consecutive uncovered lines", () => {
  const model = buildSource(file([1, 0, 0, 0, 1]));
  expect(model.blocks).toHaveLength(1);
  expect(model.blocks[0]).toMatchObject({ start: 2, end: 4, lines: 3 });
  expect(model.missLines).toBe(3);
});

test("a non-statement between two misses splits the block, as source.go does", () => {
  const model = buildSource(file([1, 0, 0, null, 0]));
  expect(model.blocks.map((b) => [b.start, b.end])).toEqual([
    [2, 3],
    [5, 5],
  ]);
  expect(model.missLines).toBe(3);
});

test("every line keeps the index of the block it belongs to", () => {
  const model = buildSource(file([1, 0, 0, 1, 0]));
  expect(lines(model).map((l) => l.block)).toEqual([null, 0, 0, null, 1]);
});

test("a fully covered file has no blocks and folds nothing", () => {
  const model = buildSource(file(Array<number>(40).fill(1)));
  expect(model.blocks).toEqual([]);
  expect(model.missLines).toBe(0);
  expect(folds(model)).toEqual([]);
  expect(lines(model)).toHaveLength(40);
  expect(lines(model).every((l) => l.fold === null)).toBe(true);
});

test("a single-line file fills the whole rail", () => {
  const model = buildSource(file([0]));
  expect(model.total).toBe(1);
  expect(model.blocks).toEqual([{ start: 1, end: 1, lines: 1, top: 0, height: 100 }]);
});

test("a long miss-free run folds, and its bar comes before the lines it hides", () => {
  const model = buildSource(file([0, ...Array<number>(FOLD_MIN_LINES + 2).fill(1), 0]));
  expect(folds(model)).toHaveLength(1);
  expect(folds(model)[0]!.fold).toEqual({ id: "f1", from: 2, to: 13, count: 12 });
  expect(model.items[0]).toMatchObject({ kind: "line" });
  expect(model.items[1]).toMatchObject({ kind: "fold" });
  const folded = lines(model).filter((l) => l.fold === "f1");
  expect(folded.map((l) => l.line.no)).toEqual([2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13]);
});

test("a run one line short of the threshold stays open", () => {
  const model = buildSource(file([0, ...Array<number>(FOLD_MIN_LINES - 1).fill(1), 0]));
  expect(folds(model)).toEqual([]);
  expect(lines(model).every((l) => l.fold === null)).toBe(true);
});

test("non-statement lines fold the same way covered ones do, and folds are numbered", () => {
  const model = buildSource(file([0, ...Array<number | null>(12).fill(null), 0, ...Array<number>(12).fill(1), 0]));
  expect(folds(model).map((f) => f.fold.id)).toEqual(["f1", "f2"]);
  expect(folds(model).map((f) => f.fold.count)).toEqual([12, 12]);
});

test("the rail places a block by its share of the file", () => {
  const hits: (number | null)[] = Array<number>(100).fill(1);
  for (let i = 40; i < 50; i++) hits[i] = 0;
  const model = buildSource(file(hits));
  expect(model.blocks[0]).toMatchObject({ start: 41, end: 50, top: 40, height: 10 });
});

test("a one-line miss in a long file keeps a clickable height", () => {
  const hits: (number | null)[] = Array<number>(1000).fill(1);
  hits[499] = 0;
  const model = buildSource(file(hits));
  expect(model.blocks[0]!.top).toBe(49.9);
  expect(model.blocks[0]!.height).toBe(0.8);
});

test("an empty file reads as nothing at all", () => {
  expect(buildSource([])).toEqual({ total: 0, missLines: 0, blocks: [], items: [] });
});
