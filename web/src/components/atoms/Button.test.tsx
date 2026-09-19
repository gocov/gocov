import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { Button, LinkButton } from "./Button";

test("clicks, and carries its variant as a class", async () => {
  const onClick = vi.fn();
  render(
    <Button variant="primary" onClick={onClick}>
      Save
    </Button>,
  );
  const b = screen.getByRole("button", { name: "Save" });
  expect(b).toHaveClass("Button", "Button--primary");
  await userEvent.click(b);
  expect(onClick).toHaveBeenCalledOnce();
});

test("LinkButton keeps in-app links in the router and sends href links out", () => {
  render(
    <MemoryRouter>
      <LinkButton to="/repos/github/acme/api">Open</LinkButton>
      <LinkButton href="https://docs.gocov.dev" external>
        Docs
      </LinkButton>
    </MemoryRouter>,
  );
  expect(screen.getByRole("link", { name: "Open" })).toHaveAttribute("href", "/repos/github/acme/api");
  expect(screen.getByRole("link", { name: "Docs" })).toHaveAttribute("target", "_blank");
});

test("a loading button spins in place of its icon and cannot be pressed", async () => {
  const onClick = vi.fn();
  const { container } = render(
    <Button icon="copy" loading onClick={onClick}>
      Saving…
    </Button>,
  );
  const b = screen.getByRole("button", { name: "Saving…" });
  expect(b).toBeDisabled();
  expect(b).toHaveAttribute("aria-busy", "true");
  expect(container.querySelector(".Spinner")).not.toBeNull();
  expect(container.querySelector(".Icon")).toBeNull();
  await userEvent.click(b);
  expect(onClick).not.toHaveBeenCalled();
});

test("a button at rest is not busy", () => {
  render(<Button>Save</Button>);
  expect(screen.getByRole("button", { name: "Save" })).not.toHaveAttribute("aria-busy");
});

test("LinkButton hands handlers and DOM attributes to the link, in the app and out of it", async () => {
  const onClick = vi.fn((e: { preventDefault: () => void }) => e.preventDefault());
  render(
    <MemoryRouter>
      <LinkButton to="/onboarding" onClick={onClick} data-step="pick">
        Choose
      </LinkButton>
      <LinkButton href="https://github.com/apps/gocov" onClick={onClick} aria-describedby="hint" className="extra">
        Install
      </LinkButton>
    </MemoryRouter>,
  );
  const inApp = screen.getByRole("link", { name: "Choose" });
  const out = screen.getByRole("link", { name: "Install" });
  expect(inApp).toHaveAttribute("data-step", "pick");
  expect(out).toHaveAttribute("aria-describedby", "hint");
  expect(out).toHaveClass("Button", "extra");
  await userEvent.click(inApp);
  await userEvent.click(out);
  expect(onClick).toHaveBeenCalledTimes(2);
});
