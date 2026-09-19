import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Toggle } from "./Toggle";

test("reports its state and asks for the opposite one", async () => {
  const onChange = vi.fn();
  render(<Toggle checked={false} onChange={onChange} label="Minimum total coverage" />);
  const sw = screen.getByRole("switch", { name: "Minimum total coverage" });
  expect(sw).toHaveAttribute("aria-checked", "false");
  await userEvent.click(sw);
  expect(onChange).toHaveBeenCalledWith(true);
});

test("a disabled switch does not fire", async () => {
  const onChange = vi.fn();
  render(<Toggle checked onChange={onChange} label="Gate" disabled />);
  await userEvent.click(screen.getByRole("switch", { name: "Gate" }));
  expect(onChange).not.toHaveBeenCalled();
});
