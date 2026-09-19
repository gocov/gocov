import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Checkbox } from "./Checkbox";

test("the label toggles the box", async () => {
  const onChange = vi.fn();
  render(<Checkbox label="Public reports" onChange={onChange} />);
  const box = screen.getByRole("checkbox", { name: "Public reports" });
  expect(box).not.toBeChecked();
  await userEvent.click(screen.getByText("Public reports"));
  expect(box).toBeChecked();
  expect(onChange).toHaveBeenCalledOnce();
});
