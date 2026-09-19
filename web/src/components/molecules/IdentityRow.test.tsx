import { render, screen } from "@testing-library/react";
import { Avatar, Chip } from "../atoms";
import { IdentityRow } from "./IdentityRow";

test("puts the identity, what it does and its status in one row", () => {
  render(
    <IdentityRow
      avatar={<Avatar kind="bot" />}
      id="gocov[bot]"
      description="Posting through the app install"
      chip={<Chip tone="plain">App install</Chip>}
    />,
  );
  expect(screen.getByText("gocov[bot]")).toBeInTheDocument();
  expect(screen.getByText("Posting through the app install")).toBeInTheDocument();
  expect(screen.getByText("App install")).toBeInTheDocument();
});
