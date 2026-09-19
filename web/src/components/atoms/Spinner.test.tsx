import { render, screen } from "@testing-library/react";
import { Spinner } from "./Spinner";

test("announces itself as busy", () => {
  render(<Spinner label="Revealing token" />);
  expect(screen.getByRole("status")).toHaveAccessibleName("Revealing token");
});
