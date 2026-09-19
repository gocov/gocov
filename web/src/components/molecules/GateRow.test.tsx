import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { GateRow } from "./GateRow";

const rule = { name: "Minimum total coverage", help: "Fails when the project drops below this figure." };

test("an off rule dims and disables its figure until the switch goes on", async () => {
  const onChange = vi.fn();
  const { container } = render(<GateRow {...rule} value={null} onChange={onChange} whenOn={80} />);

  expect(container.firstChild).toHaveClass("GateRow--off");
  expect(screen.getByRole("spinbutton", { name: rule.name })).toBeDisabled();

  await userEvent.click(screen.getByRole("switch", { name: rule.name }));
  expect(onChange).toHaveBeenCalledWith(80);
});

test("typing a figure reports it, clearing the box switches the rule off", async () => {
  const onChange = vi.fn();
  render(<GateRow {...rule} value={80} onChange={onChange} />);
  const input = screen.getByRole("spinbutton", { name: rule.name });

  await userEvent.type(input, "{backspace}5");
  expect(onChange).toHaveBeenLastCalledWith(85);

  await userEvent.clear(input);
  await userEvent.tab();
  expect(onChange).toHaveBeenLastCalledWith(null);
});

test("a read-only rule shows the figure without a switch", () => {
  render(<GateRow {...rule} value={70} onChange={vi.fn()} readOnly />);
  expect(screen.getByRole("spinbutton", { name: rule.name })).toHaveValue(70);
  expect(screen.queryByRole("switch")).not.toBeInTheDocument();
});
