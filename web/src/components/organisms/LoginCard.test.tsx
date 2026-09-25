import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { LoginInfo } from "@/lib/api/types";
import { LoginCard, trackedText } from "./LoginCard";

const info: LoginInfo = {
  hosted: false,
  providers: [
    { name: "github" },
    { name: "gitlab" },
  ],
  tracked_workspaces: [],
};

const show = (props: Partial<Parameters<typeof LoginCard>[0]> = {}) =>
  render(
    <MemoryRouter>
      <LoginCard info={info} next="/" error={null} {...props} />
    </MemoryRouter>,
  );

test("offers one sign-in button per provider, pointed at the server route", () => {
  show();
  expect(screen.getByRole("heading", { name: "Sign in to gocov" })).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Sign in with GitHub" })).toHaveAttribute(
    "href",
    "/oauth/github/start?next=%2F",
  );
  expect(screen.getByRole("link", { name: "Sign in with GitLab" })).toBeInTheDocument();
  expect(screen.getByText(/Use the account that is a member/)).toBeInTheDocument();
  expect(screen.getByText(/Access is granted by workspace membership/)).toBeInTheDocument();
});

test("a hosted instance invites anyone in", () => {
  show({ info: { ...info, hosted: true } });
  expect(screen.getByText(/Sign in to get started/)).toBeInTheDocument();
  expect(screen.getByText("Your first workspace is registered right after sign-in.")).toBeInTheDocument();
});

test("a denied account is told whom to ask, and which workspaces are tracked", () => {
  show({
    error: "denied",
    info: {
      ...info,
      tracked_workspaces: [
        { name: "acme", forge: "github" },
        { name: "acme-labs", forge: "github" },
        { name: "beta", forge: "gitlab" },
      ],
    },
  });
  expect(screen.getByRole("alert")).toHaveTextContent("Your account has no access to this instance.");
  expect(screen.getByText("Tracked workspaces: acme, acme-labs on GitHub; beta on GitLab")).toBeInTheDocument();
});

test("a denial with nothing to disclose says only that", () => {
  show({ error: "denied" });
  expect(screen.getByRole("alert")).toBeInTheDocument();
  expect(screen.queryByText(/Tracked workspaces/)).not.toBeInTheDocument();
});

test("a failed sign-in asks for another try", () => {
  show({ error: "failed" });
  expect(screen.getByRole("alert")).toHaveTextContent("Sign-in did not complete. Please try again.");
});

test("an instance with no provider configured says so and points home", () => {
  show({ info: { ...info, providers: [] } });
  expect(screen.getByText(/Sign-in is not configured on this instance/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Go to the dashboard" })).toHaveAttribute("href", "/");
  expect(screen.queryByRole("link", { name: /Sign in with/ })).not.toBeInTheDocument();
});

test("tracked workspaces group by the forge they are on", () => {
  expect(trackedText([])).toBe("");
  expect(trackedText([{ name: "acme", forge: "github" }])).toBe("acme on GitHub");
  expect(trackedText([{ name: "acme", forge: "codeberg" }])).toBe("acme on Codeberg");
});
