import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TokenCard } from "./TokenCard";

beforeAll(() => {
  HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function close(this: HTMLDialogElement) {
    this.open = false;
  };
});

function show(props: Partial<Parameters<typeof TokenCard>[0]> = {}) {
  const onReveal = vi.fn(async () => "gocov_live_9f2c41d8a7b3");
  const onRotate = vi.fn(async () => "gocov_live_0000cafebabe");
  render(
    <TokenCard
      title="Uploads"
      intro="Every repository under acme/ uploads with this token."
      serverUrl={null}
      tokenMasked="gocov_live_••••"
      owner
      onReveal={onReveal}
      onRotate={onRotate}
      {...props}
    />,
  );
  return { onReveal, onRotate };
}

test("shows GOCOV_SERVER only where the instance has one", () => {
  const { rerender } = render(
    <TokenCard
      title="Uploads"
      intro="intro"
      serverUrl={null}
      tokenMasked="gocov_live_••••"
      owner
      onReveal={async () => "t"}
      onRotate={async () => "t"}
    />,
  );
  expect(screen.queryByText("GOCOV_SERVER")).not.toBeInTheDocument();

  rerender(
    <TokenCard
      title="Uploads"
      intro="intro"
      serverUrl="https://gocov.example.com"
      tokenMasked="gocov_live_••••"
      owner
      onReveal={async () => "t"}
      onRotate={async () => "t"}
    />,
  );
  expect(screen.getByText("GOCOV_SERVER")).toBeInTheDocument();
  expect(screen.getByText("https://gocov.example.com")).toBeInTheDocument();
});

test("an owner reveals the token on demand, never before", async () => {
  const user = userEvent.setup();
  const { onReveal } = show();
  expect(onReveal).not.toHaveBeenCalled();

  await user.click(screen.getByRole("button", { name: "Reveal" }));
  expect(await screen.findByText("gocov_live_9f2c41d8a7b3")).toBeInTheDocument();
  expect(onReveal).toHaveBeenCalledOnce();
});

test("a member sees the locked field and cannot rotate or reveal", () => {
  const { onReveal } = show({ owner: false, tokenMasked: null });
  expect(screen.getByText("GOCOV_TOKEN")).toBeInTheDocument();
  expect(screen.getByText("Shown to workspace owners only")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reveal" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Rotate token" })).not.toBeInTheDocument();
  expect(screen.getByText("Only a workspace owner can see or rotate the token.")).toBeInTheDocument();
  expect(onReveal).not.toHaveBeenCalled();
});

test("rotating is confirmed first, then the new token is shown once", async () => {
  const user = userEvent.setup();
  const { onRotate } = show();
  const rotateButtons = () => screen.getAllByRole("button", { name: "Rotate token" });

  await user.click(rotateButtons()[0]!);
  expect(onRotate).not.toHaveBeenCalled();
  expect(screen.getByText("The current token stops working immediately and CI must be updated.")).toBeInTheDocument();

  await user.click(rotateButtons().at(-1)!);
  expect(onRotate).toHaveBeenCalledOnce();
  expect(await screen.findByText("gocov_live_0000cafebabe")).toBeInTheDocument();
  expect(screen.getByText(/Save it now — it is shown only this once\./)).toBeInTheDocument();
  expect(
    screen.getByText(/The previous token stopped working the moment it was rotated\./),
  ).toBeInTheDocument();
});

test("a failed rotation says so and shows no token", async () => {
  const user = userEvent.setup();
  show({
    onRotate: vi.fn(async () => {
      throw new Error("Only a workspace owner can rotate the token.");
    }),
  });

  await user.click(screen.getAllByRole("button", { name: "Rotate token" })[0]!);
  await user.click(screen.getAllByRole("button", { name: "Rotate token" }).at(-1)!);
  expect(await screen.findByRole("alert")).toHaveTextContent("Only a workspace owner can rotate the token.");
});
