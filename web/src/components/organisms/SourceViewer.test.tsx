import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SourceLine } from "@/lib/api/types";
import { SourceViewer } from "./SourceViewer";

/** hits: null = not a statement, 0 = never executed. */
const line = (no: number, hits: number | null, text: string, newMiss = false): SourceLine => ({
  no,
  text,
  hits,
  new_miss: newMiss,
});

/** Three miss blocks, with a 12-line covered run between the last two. */
const fixture: SourceLine[] = [
  line(1, 3, "func Handle() {"),
  line(2, 0, "\tif err != nil {"),
  line(3, 0, "\t\treturn err"),
  line(4, null, ""),
  line(5, 0, "\tlog.Fatal(err)", true),
  line(6, 1, "\tok := true"),
  ...Array.from({ length: 12 }, (_, i) => line(7 + i, 1, `\tstep ${i + 1}()`)),
  line(19, 0, "\tpanic(\"unreachable\")"),
  line(20, 2, "}"),
];

const pos = () => screen.queryByText(/^Block \d+ of \d+$/)?.textContent ?? "";

test("summarises the misses and maps the whole file", () => {
  render(<SourceViewer lines={fixture} newlyUncovered={1} />);
  expect(screen.getByText("4 uncovered lines in 3 blocks")).toBeInTheDocument();
  expect(screen.getByText("The rail maps all 20 lines of the file.")).toBeInTheDocument();
  expect(screen.getByText("Newly uncovered by this commit")).toBeInTheDocument();
  expect(pos()).toBe("");
});

test("a fully covered file says so and offers nothing to jump to", () => {
  render(<SourceViewer lines={[line(1, 1, "a := 1"), line(2, 2, "b := 2")]} newlyUncovered={0} />);
  expect(screen.getByText("Every statement in this file is covered.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Next miss" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Previous miss" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Uncovered only" })).toBeDisabled();
  expect(screen.queryByText("Newly uncovered by this commit")).not.toBeInTheDocument();
});

test("next and previous cycle through the blocks", async () => {
  render(<SourceViewer lines={fixture} newlyUncovered={1} />);
  const next = screen.getByRole("button", { name: "Next miss" });

  await userEvent.click(next);
  expect(pos()).toBe("Block 1 of 3");
  await userEvent.click(next);
  expect(pos()).toBe("Block 2 of 3");
  await userEvent.click(next);
  expect(pos()).toBe("Block 3 of 3");
  await userEvent.click(next);
  expect(pos()).toBe("Block 1 of 3");

  await userEvent.click(screen.getByRole("button", { name: "Previous miss" }));
  expect(pos()).toBe("Block 3 of 3");
});

test("a rail marker jumps straight to its block", async () => {
  render(<SourceViewer lines={fixture} newlyUncovered={1} />);
  expect(screen.getByRole("group", { name: "Uncovered blocks" })).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Lines 5–5 · 1 uncovered" }));
  expect(pos()).toBe("Block 2 of 3");
  await userEvent.click(screen.getByRole("button", { name: "Lines 2–3 · 2 uncovered" }));
  expect(pos()).toBe("Block 1 of 3");
});

// Testing Library normalizes whitespace, so line text is matched unindented.
test("a long covered run folds, and expanding it reveals exactly those lines", async () => {
  render(<SourceViewer lines={fixture} newlyUncovered={1} />);
  expect(screen.getByText("13 lines hidden")).toBeInTheDocument();
  expect(screen.queryByText("step 1()")).not.toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "Expand 13 hidden lines, 6–18" }));
  expect(screen.getByText("step 1()")).toBeInTheDocument();
  expect(screen.getByText("step 12()")).toBeInTheDocument();
  expect(screen.getByText("ok := true")).toBeInTheDocument();
  expect(screen.queryByText("13 lines hidden")).not.toBeInTheDocument();
});

test("uncovered only hides the covered lines and the fold bars", async () => {
  render(<SourceViewer lines={fixture} newlyUncovered={1} />);
  expect(screen.getByText("func Handle() {")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "Uncovered only" }));
  expect(screen.getByRole("button", { name: "Uncovered only" })).toHaveAttribute("aria-pressed", "true");
  expect(screen.queryByText("func Handle() {")).not.toBeInTheDocument();
  expect(screen.queryByText("13 lines hidden")).not.toBeInTheDocument();
  expect(screen.getByText("return err")).toBeInTheDocument();
  expect(screen.getByText('panic("unreachable")')).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "All lines" }));
  expect(screen.getByText("func Handle() {")).toBeInTheDocument();
});

test("hit counts read as multipliers and non-statements stay blank", () => {
  render(<SourceViewer lines={fixture} newlyUncovered={1} />);
  expect(screen.getByText("3×")).toBeInTheDocument();
  expect(screen.getAllByText("0").length).toBeGreaterThan(0);
  expect(screen.getByText("newly uncovered")).toBeInTheDocument();
});
