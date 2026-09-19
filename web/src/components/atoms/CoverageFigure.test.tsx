import { render, screen } from "@testing-library/react";
import { CoverageFigure } from "./CoverageFigure";

test("shows one decimal, coloured by the threshold", () => {
  const { container } = render(<CoverageFigure value={84.216} />);
  expect(screen.getByText("84.2%")).toBeInTheDocument();
  expect(container.firstChild).toHaveClass("CoverageFigure--good", "CoverageFigure--lg");
});

test("the medium size is a class, and no coverage is a dash", () => {
  const { container } = render(<CoverageFigure value={null} size="md" />);
  expect(container.textContent).toBe("—");
  expect(container.firstChild).toHaveClass("CoverageFigure--md", "CoverageFigure--none");
});
