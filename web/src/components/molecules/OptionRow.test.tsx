import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Avatar, Button } from "../atoms";
import { OptionRow } from "./OptionRow";

test("offers exactly one action for the thing it names", async () => {
  const onClick = vi.fn();
  render(
    <OptionRow
      avatar={<Avatar kind="forge" forge="gitlab" />}
      name="acme-labs"
      status="4 repositories · not registered"
      action={<Button onClick={onClick}>Register</Button>}
    />,
  );
  expect(screen.getByText("acme-labs")).toBeInTheDocument();
  expect(screen.getByText("4 repositories · not registered")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Register" }));
  expect(onClick).toHaveBeenCalledOnce();
});
