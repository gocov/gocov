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

test("tells its owner the copy happened, and whether it reached the clipboard", async () => {
  const onCopied = vi.fn();
  render(<CopyButton value="snippet" variant="primary" onCopied={onCopied} />);
  const button = screen.getByRole("button", { name: "Copy" });
  expect(button).toHaveClass("Button--primary");
  await userEvent.setup().click(button);
  expect(onCopied).toHaveBeenCalledExactlyOnceWith(true);
});

test("a blocked clipboard is still a copy that was asked for", async () => {
  const original = navigator.clipboard;
  Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
  const onCopied = vi.fn();
  render(<CopyButton value="snippet" onCopied={onCopied} />);
  await userEvent.click(screen.getByRole("button", { name: "Copy" }));
  expect(onCopied).toHaveBeenCalledExactlyOnceWith(false);
  Object.defineProperty(navigator, "clipboard", { value: original, configurable: true });
});

test("a value that could not be fetched was never copied", async () => {
  const onCopied = vi.fn();
  render(<CopyButton value={() => Promise.reject(new Error("forbidden"))} onCopied={onCopied} />);
  await userEvent.setup().click(screen.getByRole("button", { name: "Copy" }));
  expect(onCopied).not.toHaveBeenCalled();
});
