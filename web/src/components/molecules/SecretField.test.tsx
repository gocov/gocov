import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SecretField } from "./SecretField";

test("fetches the secret once, on the first reveal, and hides it again", async () => {
  const user = userEvent.setup();
  const onReveal = vi.fn(async () => "gocov_live_9f2c");
  render(<SecretField name="GOCOV_TOKEN" kind="Secret" masked="gocov_live_••••" onReveal={onReveal} />);

  expect(screen.getByText("gocov_live_••••")).toBeInTheDocument();
  expect(onReveal).not.toHaveBeenCalled();

  await user.click(screen.getByRole("button", { name: "Reveal" }));
  expect(screen.getByText("gocov_live_9f2c")).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Hide" }));
  expect(screen.getByText("gocov_live_••••")).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Reveal" }));
  expect(onReveal).toHaveBeenCalledOnce();
});

test("copying a hidden secret reveals it first, and never leaves it in the replay", async () => {
  const user = userEvent.setup();
  const onReveal = vi.fn(async () => "gocov_live_9f2c");
  const { container } = render(<SecretField name="GOCOV_TOKEN" kind="Secret" masked="•••" onReveal={onReveal} />);

  await user.click(screen.getByRole("button", { name: "Copy" }));
  expect(await navigator.clipboard.readText()).toBe("gocov_live_9f2c");
  expect(container.querySelector("[data-ph-no-capture]")).toBeInTheDocument();
});

test("a failed reveal says so and keeps the value masked", async () => {
  const user = userEvent.setup();
  render(
    <SecretField
      name="GOCOV_TOKEN"
      kind="Secret"
      masked="•••"
      onReveal={() => Promise.reject(new Error("Only a workspace owner can see the token."))}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Reveal" }));
  expect(screen.getByRole("alert")).toHaveTextContent("Only a workspace owner can see the token.");
  expect(screen.getByText("•••")).toBeInTheDocument();
});

test("a locked field is a header and nothing more", () => {
  render(<SecretField name="GOCOV_TOKEN" kind="Secret" note="Shown to workspace owners only" locked />);
  expect(screen.getByText("GOCOV_TOKEN")).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});

test("a variable shows its plain value without a reveal step", () => {
  render(<SecretField name="GOCOV_SERVER" kind="Variable" value="https://gocov.example.com" />);
  expect(screen.getByText("https://gocov.example.com")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reveal" })).not.toBeInTheDocument();
});
