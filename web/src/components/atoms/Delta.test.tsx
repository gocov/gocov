import { render, screen } from "@testing-library/react";
import { Delta } from "./Delta";

test.each([
  [2.4, "+2.4%", "Delta--up"],
  [-1.1, "−1.1%", "Delta--down"],
])("%s reads as %s", (value, text, cls) => {
  const { container } = render(<Delta value={value} />);
  expect(screen.getByText(text)).toBeInTheDocument();
  expect(container.firstChild).toHaveClass(cls);
});

test("a change that rounds to nothing is a quiet dash, named for a screen reader", () => {
  const { container } = render(<Delta value={0.01} />);
  expect(container.textContent).toBe("—");
  expect(screen.getByRole("img", { name: "No change" })).toHaveClass("Delta--flat");
});

test("no baseline is a dash, not a zero", () => {
  const { container } = render(<Delta value={null} />);
  expect(container.textContent).toBe("—");
  expect(screen.getByRole("img", { name: "No baseline" })).toHaveClass("Delta--none");
});
