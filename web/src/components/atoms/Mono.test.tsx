import { render, screen } from "@testing-library/react";
import { Mono } from "./Mono";

test("keeps the text, adds the face", () => {
  render(<Mono title="commit">a1b2c3d4e5f6</Mono>);
  const el = screen.getByText("a1b2c3d4e5f6");
  expect(el).toHaveClass("Mono");
  expect(el).toHaveAttribute("title", "commit");
});
