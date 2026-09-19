import { render, screen } from "@testing-library/react";
import { KeyValue, KeyValueList } from "@/components/molecules";
import type { Verdict } from "@/lib/api/types";
import { VerdictCard } from "./VerdictCard";

const verdict = (over: Partial<Verdict> = {}): Verdict => ({
  state: "pass",
  coverage: 82.3,
  delta: 1.4,
  reason: "Total coverage 82.3% is at or above the minimum of 80%.",
  base: null,
  ...over,
});

test("a passing gate reads as passing, in words", () => {
  render(<VerdictCard verdict={verdict()} />);
  expect(screen.getByText("Gate passing")).toBeInTheDocument();
  expect(screen.getByText("82.3%")).toBeInTheDocument();
  expect(screen.getByText("+1.4%")).toBeInTheDocument();
  expect(screen.getByText(/at or above the minimum/)).toBeInTheDocument();
});

test("a failing gate and a repo without one say so", () => {
  const { unmount } = render(<VerdictCard verdict={verdict({ state: "fail", delta: -2.2 })} />);
  expect(screen.getByText("Gate failing")).toBeInTheDocument();
  expect(screen.getByText("−2.2%")).toBeInTheDocument();
  unmount();

  render(<VerdictCard verdict={verdict({ state: "neutral", delta: null })} />);
  expect(screen.getByText("Coverage recorded")).toBeInTheDocument();
});

test("the meta slot sits beside the verdict", () => {
  render(
    <VerdictCard
      verdict={verdict()}
      meta={
        <KeyValueList>
          <KeyValue label="Branch" value="main" />
        </KeyValueList>
      }
    />,
  );
  expect(screen.getByText("Branch")).toBeInTheDocument();
  expect(screen.getByText("main")).toBeInTheDocument();
});
