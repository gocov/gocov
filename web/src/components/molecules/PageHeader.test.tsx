import { render, screen } from "@testing-library/react";
import { Button, Mono } from "../atoms";
import { PageHeader } from "./PageHeader";

test("the title is the page's only h1, meta and actions ride along", () => {
  render(
    <PageHeader
      title={<Mono>acme/api</Mono>}
      meta="Updated 3 hours ago"
      actions={<Button>Settings</Button>}
    />,
  );
  expect(screen.getByRole("heading", { level: 1, name: "acme/api" })).toBeInTheDocument();
  expect(screen.getByText("Updated 3 hours ago")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Settings" })).toBeInTheDocument();
});

test("a bare title leaves the meta line out", () => {
  const { container } = render(<PageHeader title="Dashboard" />);
  expect(container.querySelector(".PageHeader__meta")).toBeNull();
  expect(container.querySelector(".PageHeader__actions")).toBeNull();
});
