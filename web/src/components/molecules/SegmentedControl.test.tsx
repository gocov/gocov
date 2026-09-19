import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SegmentedControl } from "./SegmentedControl";

const options = [
  { value: "all", label: "All", count: 28 },
  { value: "changed", label: "Changed", count: 4 },
  { value: "source", label: "Source changed", disabled: true },
];

test("marks the selected option and reports the next one", async () => {
  const onChange = vi.fn();
  render(<SegmentedControl label="Filter files" options={options} value="all" onChange={onChange} />);
  expect(screen.getByRole("group", { name: "Filter files" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /^All/ })).toHaveAttribute("aria-pressed", "true");
  await userEvent.click(screen.getByRole("button", { name: /^Changed/ }));
  expect(onChange).toHaveBeenCalledWith("changed");
});

test("a disabled option cannot be chosen", async () => {
  const onChange = vi.fn();
  render(<SegmentedControl label="Filter files" options={options} value="all" onChange={onChange} />);
  await userEvent.click(screen.getByRole("button", { name: "Source changed" }));
  expect(onChange).not.toHaveBeenCalled();
});
