import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CopyButton } from "./CopyButton";

test("copies the text and says so, then goes back", async () => {
  const user = userEvent.setup();
  render(<CopyButton value="gocov_tok_123" />);

  await user.click(screen.getByRole("button", { name: "Copy" }));
  expect(await navigator.clipboard.readText()).toBe("gocov_tok_123");
  expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();

  // The confirmation is momentary — the button goes back to offering the copy.
  await screen.findByRole("button", { name: "Copy" }, { timeout: 3000 });
});

test("asks for the value only when it is needed", async () => {
  const user = userEvent.setup();
  const reveal = vi.fn(async () => "fetched-secret");
  render(<CopyButton value={reveal} label="Copy token" />);
  expect(reveal).not.toHaveBeenCalled();

  await user.click(screen.getByRole("button", { name: "Copy token" }));
  expect(reveal).toHaveBeenCalledOnce();
  expect(await navigator.clipboard.readText()).toBe("fetched-secret");
});

test("survives a browser with no clipboard at all", async () => {
  const original = navigator.clipboard;
  Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
  render(<CopyButton value="nothing happens" />);
  await userEvent.click(screen.getByRole("button", { name: "Copy" }));
  expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument();
  Object.defineProperty(navigator, "clipboard", { value: original, configurable: true });
});
