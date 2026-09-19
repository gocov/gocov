import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { Breadcrumbs } from "./Breadcrumbs";

test("links every step but the one you are on", () => {
  render(
    <MemoryRouter>
      <Breadcrumbs
        items={[
          { label: "acme", to: "/?ws=github/acme" },
          { label: "acme/api", to: "/repos/github/acme/api" },
          { label: "Upload 412" },
        ]}
      />
    </MemoryRouter>,
  );
  expect(screen.getByRole("navigation", { name: "Breadcrumb" })).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "acme/api" })).toHaveAttribute("href", "/repos/github/acme/api");
  expect(screen.queryByRole("link", { name: "Upload 412" })).not.toBeInTheDocument();
  expect(screen.getByText("Upload 412")).toHaveAttribute("aria-current", "page");
});
