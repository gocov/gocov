import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Select } from "./Select";

test("picks an option", async () => {
  const onChange = vi.fn();
  render(
    <Select aria-label="Keep reports for" defaultValue="90" onChange={onChange}>
      <option value="90">90 days</option>
      <option value="365">1 year</option>
      <option value="0">Forever</option>
    </Select>,
  );
  const select = screen.getByRole("combobox", { name: "Keep reports for" });
  await userEvent.selectOptions(select, "0");
  expect(select).toHaveValue("0");
  expect(onChange).toHaveBeenCalled();
});
