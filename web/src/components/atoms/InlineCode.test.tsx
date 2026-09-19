import { render, screen } from "@testing-library/react";
import { InlineCode } from "./InlineCode";

test("renders as a code element", () => {
  render(<InlineCode>GOCOV_TOKEN</InlineCode>);
  expect(screen.getByText("GOCOV_TOKEN").tagName).toBe("CODE");
});
