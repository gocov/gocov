import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import type { Gate } from "@/lib/api/types";
import { GatesCard } from "./GatesCard";

const empty: Gate = { min_coverage: null, min_diff_coverage: null, max_coverage_drop: null };

function Harness({
  start = empty,
  scope = "workspace",
  readOnly = false,
}: {
  start?: Gate;
  scope?: "workspace" | "repo";
  readOnly?: boolean;
}) {
  const [gate, setGate] = useState<Gate>(start);
  return <GatesCard gate={gate} scope={scope} readOnly={readOnly} onChange={setGate} footer={<span>Applies to the next upload.</span>} />;
}

test("the header chip counts the rules as they are switched on and off", async () => {
  const user = userEvent.setup();
  render(<Harness />);
  expect(screen.getByText("None active")).toBeInTheDocument();

  await user.click(screen.getByRole("switch", { name: "Minimum total coverage" }));
  expect(screen.getByText("1 active")).toBeInTheDocument();
  expect(screen.getByRole("spinbutton", { name: "Minimum total coverage" })).toHaveValue(80);

  await user.click(screen.getByRole("switch", { name: "Minimum diff coverage" }));
  expect(screen.getByText("2 active")).toBeInTheDocument();
  expect(screen.getByRole("spinbutton", { name: "Minimum diff coverage" })).toHaveValue(70);

  await user.click(screen.getByRole("switch", { name: "Minimum total coverage" }));
  expect(screen.getByText("1 active")).toBeInTheDocument();
});

test("the scope decides whose repositories the gates cover", () => {
  const { rerender } = render(<Harness scope="workspace" />);
  expect(
    screen.getByText(/starting gates of every repository registered in this workspace from now on; existing repositories keep their own\./),
  ).toBeInTheDocument();
  expect(screen.getByText("Fails when the whole project drops below this figure.")).toBeInTheDocument();

  rerender(<Harness scope="repo" />);
  expect(screen.getByText(/They apply to this repository only\./)).toBeInTheDocument();
  expect(screen.getByText("Fails when the whole repository drops below this figure.")).toBeInTheDocument();
});

test("read-only shows the figures without a way to change them", () => {
  render(<Harness start={{ min_coverage: 80, min_diff_coverage: null, max_coverage_drop: 2 }} readOnly />);
  expect(screen.getByText("2 active")).toBeInTheDocument();
  expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  expect(screen.getByRole("spinbutton", { name: "Minimum total coverage" })).toBeDisabled();
});
