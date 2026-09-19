import { render, screen } from "@testing-library/react";
import { TextInput } from "../atoms";
import { FormField } from "./FormField";

test("the label names the control and the help describes it", () => {
  render(
    <FormField label="Default branch" help="Trends are measured against this branch.">
      {(field) => <TextInput {...field} defaultValue="main" />}
    </FormField>,
  );
  const input = screen.getByRole("textbox", { name: "Default branch" });
  expect(input).toHaveAccessibleDescription("Trends are measured against this branch.");
  expect(input).not.toHaveAttribute("aria-invalid");
});

test("an error marks the control invalid and is read with it", () => {
  render(
    <FormField label="Default branch" help="One branch name." error="That branch does not exist.">
      {(field) => <TextInput {...field} defaultValue="mian" />}
    </FormField>,
  );
  const input = screen.getByRole("textbox", { name: "Default branch" });
  expect(input).toHaveAttribute("aria-invalid", "true");
  expect(input).toHaveAccessibleDescription("One branch name. That branch does not exist.");
});
