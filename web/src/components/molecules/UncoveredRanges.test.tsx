import { render, screen } from "@testing-library/react";
import { UncoveredRanges } from "./UncoveredRanges";

test("splits the API's string and truncates a long list", () => {
  render(<UncoveredRanges ranges="12-18, 40, 55-57, 61, 70" max={3} />);
  expect(screen.getByText("12-18")).toBeInTheDocument();
  expect(screen.getByText("55-57")).toBeInTheDocument();
  expect(screen.queryByText("61")).not.toBeInTheDocument();
  expect(screen.getByText("+2 more")).toBeInTheDocument();
});

test("fully covered is a dash, not an empty cell", () => {
  const { container } = render(<UncoveredRanges ranges="" />);
  expect(container.textContent).toBe("—");
});
