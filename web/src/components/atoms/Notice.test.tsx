import { render, screen } from "@testing-library/react";
import { Notice } from "./Notice";

test("a bad notice interrupts, a neutral one does not", () => {
  const { container } = render(<Notice tone="bad">The grant stopped working.</Notice>);
  expect(screen.getByRole("alert")).toHaveTextContent("The grant stopped working.");
  expect(container.firstChild).toHaveClass("Notice--bad");
});

test("busy shows the spinner instead of the tone mark", () => {
  render(<Notice busy>Rotating the token…</Notice>);
  expect(screen.getByRole("status", { name: "Working" })).toBeInTheDocument();
});
