import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TextInput } from "./TextInput";

test("types, and stays reachable by its label", async () => {
  render(<TextInput aria-label="Default branch" defaultValue="" />);
  const input = screen.getByRole("textbox", { name: "Default branch" });
  await userEvent.type(input, "main");
  expect(input).toHaveValue("main");
});

test("search carries the magnifier, invalid is announced", () => {
  const { container } = render(<TextInput type="search" aria-label="Search files" invalid />);
  const input = screen.getByRole("searchbox", { name: "Search files" });
  expect(input).toHaveAttribute("aria-invalid", "true");
  expect(input).toHaveClass("TextInput--invalid");
  expect(container.querySelector(".TextInput__icon svg")).toBeInTheDocument();
});
