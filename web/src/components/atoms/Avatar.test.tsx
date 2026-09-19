import { render, screen } from "@testing-library/react";
import { Avatar } from "./Avatar";

test("an initial tile takes the first letter of the name", () => {
  render(<Avatar name="acme" />);
  expect(screen.getByText("A")).toBeInTheDocument();
});

test("a forge avatar draws the mark and can carry a name", () => {
  const { container } = render(<Avatar kind="forge" forge="github" label="GitHub" />);
  expect(screen.getByRole("img", { name: "GitHub" })).toBeInTheDocument();
  expect(container.querySelector(".ForgeMark")).toBeInTheDocument();
});

test("a decorative avatar is hidden from the accessibility tree", () => {
  const { container } = render(<Avatar kind="bot" />);
  expect(container.firstChild).toHaveAttribute("aria-hidden", "true");
});
