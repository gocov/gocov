import { render, screen } from "@testing-library/react";
import { Button } from "../atoms";
import { Toolbar } from "./Toolbar";

test("keeps both sides in one row", () => {
  render(<Toolbar label="File tools" left={<Button>Expand all</Button>} right="28 files" />);
  const bar = screen.getByRole("group", { name: "File tools" });
  expect(bar).toHaveTextContent("Expand all");
  expect(bar).toHaveTextContent("28 files");
});
