import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { OnboardingInfo, RegisterInput, RegisterResult } from "@/lib/api/types";
import { mockApi, renderPage } from "@/test/render";
import OnboardingPage from "./OnboardingPage";

const at = { route: "onboarding", path: "/onboarding" };

const info: OnboardingInfo = {
  forge: "bitbucket",
  forge_label: "Bitbucket",
  account: "Ada Lovelace",
  mode: "pick",
  install_url: "",
  rows: [
    { prefix: "acme", state: "available" },
    { prefix: "beta", state: "unowned" },
  ],
  membership_count: 2,
};

test("the memberships read at sign-in are the choice", async () => {
  mockApi({ "GET /onboarding": info });
  renderPage(<OnboardingPage />, at);

  expect(await screen.findByRole("heading", { name: "Choose a workspace" })).toBeInTheDocument();
  expect(document.title).toBe("set up coverage — gocov");
  expect(screen.getByText("acme")).toBeInTheDocument();
  expect(screen.getByText(/Signed in as Ada Lovelace · 2 memberships read at sign-in/)).toBeInTheDocument();
});

test("Create registers the workspace and lands on its dashboard", async () => {
  const user = userEvent.setup();
  let posted: RegisterInput | null = null;
  mockApi({
    "GET /onboarding": info,
    "POST /onboarding/register": (init: RequestInit | undefined) => {
      posted = JSON.parse(String(init?.body)) as RegisterInput;
      return { forge: "bitbucket", prefix: "acme", created: true } satisfies RegisterResult;
    },
  });
  const { router } = renderPage(<OnboardingPage />, at);

  await user.click(await screen.findByRole("button", { name: "Create" }));

  await waitFor(() => expect(router.state.location.search).toBe("?ws=bitbucket%2Facme"));
  expect(router.state.location.pathname).toBe("/");
  expect(posted).toEqual({ prefix: "acme" });
});

test("a refused registration says why, and the choice stays open", async () => {
  const user = userEvent.setup();
  mockApi({
    "GET /onboarding": info,
    "POST /onboarding/register": {
      status: 403,
      error: "creating a workspace takes an admin or owner of it on the forge",
    },
  });
  const { router } = renderPage(<OnboardingPage />, at);

  await user.click(await screen.findByRole("button", { name: "Create" }));

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "creating a workspace takes an admin or owner of it on the forge",
  );
  expect(router.state.location.pathname).toBe("/onboarding");
  expect(screen.getByRole("button", { name: "Create" })).toBeEnabled();
});

test("an instance with no registration is a 404, not an empty screen", async () => {
  mockApi({});
  renderPage(<OnboardingPage />, at);
  expect(await screen.findByText(/We couldn’t find that page/)).toBeInTheDocument();
});

test("an install that ended without a workspace explains itself above the picker", async () => {
  mockApi({ "GET /onboarding": info });
  const { router } = renderPage(<OnboardingPage />, {
    route: "onboarding",
    path: "/onboarding?connect=not_your_workspace&ws=acme&installation_id=42",
  });

  const notice = await screen.findByRole("alert");
  expect(notice).toHaveTextContent("Not your workspace");
  expect(notice).toHaveTextContent(/gocov can see the app installed on acme/);
  expect(within(notice).getByRole("link", { name: "Allow gocov on acme" })).toHaveAttribute(
    "href",
    "https://github.com/organizations/acme/settings/oauth_application_policy",
  );
  expect(within(notice).getByRole("link", { name: "Sign in again" })).toHaveAttribute(
    "href",
    "/oauth/github/start?next=%2Fgithub%2Fsetup%3Finstallation_id%3D42",
  );

  // The install is a one-shot message: the URL is clean, the message is not.
  await waitFor(() => expect(router.state.location.search).toBe(""));
  expect(screen.getByRole("alert")).toHaveTextContent("Not your workspace");
  expect(await screen.findByRole("heading", { name: "Choose a workspace" })).toBeInTheDocument();
});

test("a requested install reads as progress, not as a failure", async () => {
  mockApi({ "GET /onboarding": info });
  const { router } = renderPage(<OnboardingPage />, {
    route: "onboarding",
    path: "/onboarding?connect=install_requested",
  });

  expect(await screen.findByText("Install requested")).toBeInTheDocument();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  await waitFor(() => expect(router.state.location.search).toBe(""));
});

test("an unknown code says what it can and links nowhere", async () => {
  mockApi({ "GET /onboarding": info });
  renderPage(<OnboardingPage />, {
    route: "onboarding",
    path: "/onboarding?connect=%3Cimg%20src%3Dx%3E&ws=javascript%3Aalert(1)&installation_id=x",
  });

  const notice = await screen.findByRole("alert");
  expect(notice).toHaveTextContent("The GitHub App install could not be completed.");
  expect(within(notice).queryByRole("link")).not.toBeInTheDocument();
});

test("no connect parameter, no notice", async () => {
  mockApi({ "GET /onboarding": info });
  renderPage(<OnboardingPage />, at);
  expect(await screen.findByRole("heading", { name: "Choose a workspace" })).toBeInTheDocument();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});
