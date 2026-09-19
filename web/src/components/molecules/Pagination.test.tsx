import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { Pagination } from "./Pagination";

test("the first page cannot go newer and links to older", () => {
  render(
    <MemoryRouter>
      <Pagination older={{ to: "/repos/github/acme/api?page=2" }} />
    </MemoryRouter>,
  );
  expect(screen.getByRole("navigation", { name: "Pagination" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Newer" })).toBeDisabled();
  expect(screen.getByRole("link", { name: "Older" })).toHaveAttribute("href", "/repos/github/acme/api?page=2");
});

test("a callback page turner works too", async () => {
  const onClick = vi.fn();
  render(
    <MemoryRouter>
      <Pagination newer={{ onClick }} older={{ disabled: true }} />
    </MemoryRouter>,
  );
  await userEvent.click(screen.getByRole("button", { name: "Newer" }));
  expect(onClick).toHaveBeenCalledOnce();
  expect(screen.getByRole("button", { name: "Older" })).toBeDisabled();
});
