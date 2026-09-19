import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Textarea } from "./Textarea";

test("takes several lines and can be monospaced", async () => {
  render(<Textarea aria-label="Ignore paths" mono defaultValue="" />);
  const box = screen.getByRole("textbox", { name: "Ignore paths" });
  expect(box).toHaveClass("Textarea--mono");
  await userEvent.type(box, "vendor/**{enter}**/*_test.go");
  expect(box).toHaveValue("vendor/**\n**/*_test.go");
});
