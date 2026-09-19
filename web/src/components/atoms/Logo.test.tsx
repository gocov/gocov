import { render } from "@testing-library/react";
import { Logo } from "./Logo";

test("is decorative, and as large as it is asked to be", () => {
  const { container } = render(<Logo size={34} />);
  const svg = container.querySelector("svg")!;
  expect(svg).toHaveAttribute("aria-hidden", "true");
  expect(svg).toHaveAttribute("width", "34");
  expect(svg).toHaveAttribute("height", "34");
});

test("defaults to the size the top bar uses", () => {
  const { container } = render(<Logo />);
  expect(container.querySelector("svg")).toHaveAttribute("width", "18");
});
