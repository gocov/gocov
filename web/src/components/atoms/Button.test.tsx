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
