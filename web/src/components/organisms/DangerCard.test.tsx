import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { DangerCard } from "./DangerCard";

beforeAll(() => {
  HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function close(this: HTMLDialogElement) {
    this.open = false;
  };
});

function show(props: Partial<Parameters<typeof DangerCard>[0]> = {}) {
  const onConfirm = vi.fn(async () => {});
  render(
    <DangerCard
      title="Delete workspace"
      actionLabel="Delete this workspace"
      confirmText="Delete acme and all of its coverage data? This cannot be undone."
      hint="This cannot be undone."
      ownerOnlyHint="Only a workspace owner can delete it."
      owner
      onConfirm={onConfirm}
      {...props}
    >
      <p>Removes acme, its 8 repositories and every coverage report gocov holds for them.</p>
    </DangerCard>,
  );
  return onConfirm;
}

test("the move waits for the confirmation", async () => {
  const user = userEvent.setup();
  const onConfirm = show();
  const buttons = () => screen.getAllByRole("button", { name: "Delete this workspace" });

  await user.click(buttons()[0]!);
  expect(onConfirm).not.toHaveBeenCalled();
  expect(screen.getByText("Delete acme and all of its coverage data? This cannot be undone.")).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(onConfirm).not.toHaveBeenCalled();

  await user.click(buttons()[0]!);
  await user.click(buttons().at(-1)!);
  expect(onConfirm).toHaveBeenCalledOnce();
});

test("a member is told whose move it is, and is offered no button", () => {
  show({ owner: false });
  expect(screen.getByText("Only a workspace owner can delete it.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Delete this workspace" })).not.toBeInTheDocument();
});

test("a refused move is reported and leaves the card in place", async () => {
  const user = userEvent.setup();
  show({
    onConfirm: vi.fn(async () => {
      throw new Error("Only a workspace owner can delete it.");
    }),
  });
  const buttons = () => screen.getAllByRole("button", { name: "Delete this workspace" });

  await user.click(buttons()[0]!);
  await user.click(buttons().at(-1)!);
  expect(await screen.findByRole("alert")).toHaveTextContent("Only a workspace owner can delete it.");
  expect(buttons()[0]).toBeEnabled();
});
