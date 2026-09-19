import { render, screen } from "@testing-library/react";
import { CoverageBar } from "./CoverageBar";

test.each([
  [41, "bad"],
  [63, "warn"],
  [84.2, "good"],
])("%d%% fills as %s", (value, lvl) => {
  const { container } = render(<CoverageBar value={value} />);
  expect(screen.getByRole("img")).toHaveAccessibleName(`${value.toFixed(1)}% covered`);
  expect(container.querySelector(".CoverageBar__fill")).toHaveClass(`CoverageBar__fill--${lvl}`);
});
