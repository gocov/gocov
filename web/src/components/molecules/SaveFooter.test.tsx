import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SaveFooter } from "./SaveFooter";

const rest = { hint: "Applies to the next upload.", ownerOnly: "Owners set this.", busy: false, saving: false, saved: false };

test("an owner gets Save and what it applies to", async () => {
  const onSave = vi.fn();
  render(<SaveFooter owner {...rest} onSave={onSave} />);
  expect(screen.getByText("Applies to the next upload.")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(onSave).toHaveBeenCalledOnce();
});

test("a member reads who may save instead", () => {
  render(<SaveFooter {...rest} owner={false} onSave={() => {}} />);
  expect(screen.getByText("Owners set this.")).toBeInTheDocument();
  expect(screen.queryByRole("button")).toBeNull();
});

test("the pressed card says it is saving; the others only wait", () => {
  const { rerender } = render(<SaveFooter owner {...rest} busy saving onSave={() => {}} />);
  expect(screen.getByRole("button", { name: "Saving…" })).toHaveAttribute("aria-busy", "true");
  rerender(<SaveFooter owner {...rest} busy onSave={() => {}} />);
  const waiting = screen.getByRole("button", { name: "Save" });
  expect(waiting).toBeDisabled();
  expect(waiting).not.toHaveAttribute("aria-busy");
});

test("says Saved once it worked", () => {
  render(<SaveFooter owner {...rest} saved onSave={() => {}} />);
  expect(screen.getByText("Saved")).toBeInTheDocument();
});
