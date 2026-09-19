import { render, screen } from "@testing-library/react";
import { Spinner } from "./Spinner";

test("announces itself as busy", () => {
  render(<Spinner label="Revealing token" />);
  expect(screen.getByRole("status")).toHaveAccessibleName("Revealing token");
});

test("is silent where the words around it already say so", () => {
  const { container } = render(<Spinner label={null} />);
  expect(screen.queryByRole("status")).toBeNull();
  expect(container.querySelector(".Spinner")).toHaveAttribute("aria-hidden", "true");
});
