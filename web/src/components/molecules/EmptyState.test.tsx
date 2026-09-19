import { render, screen } from "@testing-library/react";
import { Button } from "../atoms";
import { EmptyState } from "./EmptyState";

test("says what is missing and offers the way out", () => {
  render(<EmptyState message="No uploads yet." action={<Button>Set up CI</Button>} />);
  expect(screen.getByText("No uploads yet.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Set up CI" })).toBeInTheDocument();
});
