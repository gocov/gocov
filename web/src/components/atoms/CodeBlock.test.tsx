import { render, screen } from "@testing-library/react";
import { CodeBlock } from "./CodeBlock";

test("is a labelled, focusable region", () => {
  render(<CodeBlock label="Upload step">{"- run: gocov upload coverage.out"}</CodeBlock>);
  const block = screen.getByRole("group", { name: "Upload step" });
  expect(block).toHaveTextContent("gocov upload coverage.out");
  expect(block).toHaveAttribute("tabindex", "0");
});
