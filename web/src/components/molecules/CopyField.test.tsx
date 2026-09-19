import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CopyField } from "./CopyField";

test("shows the value read-only and copies it", async () => {
  const user = userEvent.setup();
  render(<CopyField label="Badge markdown" value="![coverage](https://gocov.dev/badge)" preview={<span>img</span>} />);

  const field = screen.getByRole("textbox", { name: "Badge markdown" });
  expect(field).toHaveValue("![coverage](https://gocov.dev/badge)");
  expect(field).toHaveAttribute("readonly");
  expect(screen.getByText("img")).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Copy" }));
  expect(await navigator.clipboard.readText()).toBe("![coverage](https://gocov.dev/badge)");
});
