import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { ConfirmDialog } from "./ConfirmDialog";

// jsdom ships the element without the modal methods.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function close(this: HTMLDialogElement) {
    this.open = false;
    this.dispatchEvent(new Event("close"));
  };
});

function Harness({ onConfirm }: { onConfirm: () => void }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Delete this workspace
      </button>
      <ConfirmDialog
        open={open}
        danger
        title="Delete acme?"
        confirmLabel="Delete"
        onConfirm={() => {
          setOpen(false);
          onConfirm();
        }}
        onCancel={() => setOpen(false)}
      >
        Every coverage report gocov holds for it goes too.
      </ConfirmDialog>
    </>
  );
}

test("opens from the trigger, confirms, and hands focus back", async () => {
  const user = userEvent.setup();
  const onConfirm = vi.fn();
  render(<Harness onConfirm={onConfirm} />);

  const trigger = screen.getByRole("button", { name: "Delete this workspace" });
  const dialog = document.querySelector("dialog");
  expect(dialog).not.toHaveAttribute("open");

  await user.click(trigger);
  expect(dialog).toHaveAttribute("open");
  expect(dialog).toHaveTextContent("Every coverage report gocov holds for it goes too.");

  await user.click(screen.getByRole("button", { name: "Delete" }));
  expect(onConfirm).toHaveBeenCalledOnce();
  expect(trigger).toHaveFocus();
});

test("cancelling closes it and changes nothing", async () => {
  const user = userEvent.setup();
  const onConfirm = vi.fn();
  render(<Harness onConfirm={onConfirm} />);

  await user.click(screen.getByRole("button", { name: "Delete this workspace" }));
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(onConfirm).not.toHaveBeenCalled();
  expect(document.querySelector("dialog")).not.toHaveAttribute("open");
});
