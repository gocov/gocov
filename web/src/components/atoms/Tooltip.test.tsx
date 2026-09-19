import { render, screen } from "@testing-library/react";
import { Tooltip } from "./Tooltip";

test("describes its anchor", () => {
  render(<Tooltip text="Statement-weighted across the workspace">Coverage</Tooltip>);
  const tip = screen.getByRole("tooltip");
  expect(tip).toHaveTextContent("Statement-weighted across the workspace");
  expect(screen.getByText("Coverage").closest(".Tooltip__anchor")).toHaveAttribute("aria-describedby", tip.id);
});
