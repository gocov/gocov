import { screen, waitFor } from "@testing-library/react";
import type { LoginInfo, Session } from "@/lib/api/types";
import { mockApi, renderPage } from "@/test/render";
import LoginPage from "./LoginPage";

const signedOut: Session = { user: null, auth_enabled: true, hosted: false };

const login: LoginInfo = {
  hosted: false,
  providers: [{ name: "github", label: "GitHub" }],
  tracked_workspaces: [],
};

const show = (path: string, handlers: Record<string, unknown> = {}) => {
  mockApi({ "GET /session": signedOut, "GET /login": login, ...handlers });
  return renderPage(<LoginPage />, { route: "login", path });
};

test("shows the providers and comes back to the app by default", async () => {
  show("/login");
  expect(await screen.findByRole("link", { name: "Sign in with GitHub" })).toHaveAttribute(
    "href",
    "/oauth/github/start?next=%2F",
  );
});

test("keeps the page the viewer was sent away from", async () => {
  show("/login?next=/uploads/412");
  expect(await screen.findByRole("link", { name: "Sign in with GitHub" })).toHaveAttribute(
    "href",
    "/oauth/github/start?next=%2Fuploads%2F412",
  );
});

test.each(["//evil.example", "/\\evil.example", "https://evil.example", "app/"])(
  "refuses to carry %s off this origin",
  async (next) => {
    show(`/login?next=${encodeURIComponent(next)}`);
    expect(await screen.findByRole("link", { name: "Sign in with GitHub" })).toHaveAttribute(
      "href",
      "/oauth/github/start?next=%2F",
    );
  },
);

test("a denied sign-in names the tracked workspaces", async () => {
  show("/login?denied=1", {
    "GET /login": {
      ...login,
      tracked_workspaces: [
        { name: "acme", forge: "github" },
        { name: "acme-labs", forge: "github" },
      ],
    } satisfies LoginInfo,
  });
  expect(await screen.findByRole("alert")).toHaveTextContent("Your account has no access to this instance.");
  expect(screen.getByText("Tracked workspaces: acme, acme-labs on GitHub")).toBeInTheDocument();
});

test("a failed sign-in asks for another try", async () => {
  show("/login?failed=1");
  expect(await screen.findByRole("alert")).toHaveTextContent("Sign-in did not complete. Please try again.");
});

test("an instance with no providers says sign-in is not configured", async () => {
  show("/login", { "GET /login": { ...login, providers: [] } satisfies LoginInfo });
  expect(await screen.findByText(/Sign-in is not configured on this instance/)).toBeInTheDocument();
});

test("someone already signed in is sent to the dashboard", async () => {
  const { router } = show("/login", {
    "GET /session": { ...signedOut, user: { display_name: "Ada", email: "ada@example.com" } } satisfies Session,
  });
  await waitFor(() => expect(router.state.location.pathname).toBe("/"));
  expect(screen.queryByRole("heading", { name: "Sign in to gocov" })).not.toBeInTheDocument();
});

test("the tab says what page this is", async () => {
  show("/login");
  await screen.findByRole("link", { name: "Sign in with GitHub" });
  expect(document.title).toBe("sign in — gocov");
});
