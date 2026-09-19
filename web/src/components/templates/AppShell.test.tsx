import { screen } from "@testing-library/react";
import type { Session } from "@/lib/api/types";
import { mockApi, renderPage } from "@/test/render";
import { AppShell } from "./AppShell";

const session = (over: Partial<Session> = {}): Session => ({
  user: null,
  auth_enabled: true,
  hosted: false,
  ...over,
});

test("a signed-out viewer is offered sign-in, in the app, with the page to come back to", async () => {
  mockApi({ "GET /session": session() });
  const { router } = renderPage(<AppShell />, { route: "*", path: "/repos/github/acme/api?branch=main" });

  const link = await screen.findByRole("link", { name: "Sign in" });
  expect(link).toHaveAttribute("href", "/login?next=%2Frepos%2Fgithub%2Facme%2Fapi%3Fbranch%3Dmain");

  // A route, not a page load: the router handles the click itself.
  link.click();
  expect(router.state.location.pathname).toBe("/login");
});

test("a signed-in viewer sees their name and a way out instead", async () => {
  mockApi({ "GET /session": session({ user: { display_name: "Ada", email: "ada@example.com" } }) });
  renderPage(<AppShell />, { route: "*", path: "/" });

  expect(await screen.findByText("Ada")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Sign out" })).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Sign in" })).not.toBeInTheDocument();
});
