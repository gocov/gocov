import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Dashboard, SetupInfo, SetupStatus } from "@/lib/api/types";
import { mockApi, renderPage } from "@/test/render";
import DashboardPage from "./DashboardPage";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
});

const acme: Dashboard["current"] = {
  forge: "github",
  prefix: "acme",
  repo_count: 4,
  coverage: 74.6,
  current: true,
  tracked: true,
};

const labs = { ...acme!, forge: "gitlab", prefix: "acme-labs", coverage: null, current: false, tracked: false };

const dashboard: Dashboard = {
  needs_onboarding: false,
  can_onboard: true,
  current: acme,
  switcher: [acme!, labs],
  repos: [
    {
      forge: "github",
      slug: "acme/api",
      name: "api",
      coverage: 41,
      delta: -2.4,
      gate: "fail",
      stale: false,
      series: [46, 44, 41],
      uploaded_at: "2026-09-18T09:00:00Z",
    },
    {
      forge: "github",
      slug: "acme/web",
      name: "web",
      coverage: 82.5,
      delta: 0.4,
      gate: "pass",
      stale: true,
      series: [80, 81, 82.5],
      uploaded_at: "2026-08-20T09:00:00Z",
    },
    {
      forge: "github",
      slug: "acme/tools",
      name: "tools",
      coverage: 66,
      delta: null,
      gate: "none",
      stale: false,
      series: [64, 66],
      uploaded_at: "2026-09-16T09:00:00Z",
    },
    {
      forge: "github",
      slug: "acme/docs",
      name: "docs",
      coverage: null,
      delta: null,
      gate: "none",
      stale: false,
      series: [],
      uploaded_at: null,
    },
  ],
  stats: {
    coverage: 74.6,
    gates_passing: 1,
    gates_total: 2,
    stale_count: 1,
    reporting: "connected",
    reporting_as: "gocov[bot]",
  },
  attention: [
    { kind: "failing", forge: "github", slug: "acme/api", name: "api", coverage: 41, min_coverage: 60, stale_days: null },
    { kind: "stale", forge: "github", slug: "acme/web", name: "web", coverage: 82.5, min_coverage: null, stale_days: 30 },
  ],
};

// The dashboard answers on the index route and on a workspace's own address.
const routeFor = (path: string) => (path.startsWith("/w/") ? "/w/:forge/*" : "/");

const show = (data: Dashboard, path = "/", route = routeFor(path), extra: Record<string, unknown> = {}) => {
  const fetchMock = mockApi({ "GET /dashboard": data, ...extra });
  return { fetchMock, ...renderPage(<DashboardPage />, { route, path }) };
};

test("a populated workspace shows its rollup, its notices and its repositories", async () => {
  show(dashboard);
  expect(document.title).toBe("repositories — gocov");

  const heading = await screen.findByRole("heading", { level: 1 });
  expect(within(heading).getByRole("button", { name: /acme/ })).toBeInTheDocument();
  expect(screen.getByText("4 repositories · 74.6% covered")).toBeInTheDocument();

  expect(screen.getByRole("link", { name: "Workspace settings" })).toHaveAttribute("href", "/workspace-settings/github/acme");
  expect(screen.getByRole("link", { name: "Add a repository" })).toHaveAttribute(
    "href",
    "/workspace-setup/github/acme",
  );

  // The three stat tiles.
  expect(screen.getByText("74.6%")).toBeInTheDocument();
  expect(screen.getByText("weighted by statements")).toBeInTheDocument();
  expect(screen.getByText("of 2 with a gate · 1 stale")).toBeInTheDocument();
  expect(screen.getByText("Connected")).toBeInTheDocument();
  expect(screen.getByText("gocov[bot]")).toBeInTheDocument();

  expect(screen.getByRole("heading", { name: "Needs attention" })).toBeInTheDocument();
  expect(screen.getByText("2 things")).toBeInTheDocument();
  expect(screen.getByText("Coverage 41.0%, below the 60% minimum.")).toBeInTheDocument();
  expect(screen.getAllByRole("listitem")).toHaveLength(2);

  expect(screen.getByRole("heading", { name: "Repositories" })).toBeInTheDocument();
  expect(screen.getAllByRole("row")).toHaveLength(5); // header + 4
  // Nothing under the table repeats what "Add a repository" already offers.
  expect(screen.queryByRole("link", { name: "Setup instructions" })).not.toBeInTheDocument();
});

test("the table filters without another request", async () => {
  const { fetchMock } = show(dashboard);
  await screen.findByRole("table");
  const calls = fetchMock.mock.calls.length;

  await userEvent.click(within(screen.getByRole("group", { name: "Filter repositories" })).getByRole("button", { name: /^Failing/ }));
  expect(screen.getAllByRole("row")).toHaveLength(2);
  expect(fetchMock.mock.calls.length).toBe(calls);
});

test("the path picks the workspace the query asks the server for", async () => {
  const { fetchMock } = show(dashboard, "/w/gitlab/acme-labs");
  await screen.findByRole("table");
  expect(String(fetchMock.mock.calls[0]?.[0])).toBe("/api/ui/dashboard?ws=gitlab%2Facme-labs");
});

test("a nested GitLab group is a path like any other", async () => {
  const { fetchMock } = show(dashboard, "/w/gitlab/grp/sub");
  await screen.findByRole("table");
  expect(String(fetchMock.mock.calls[0]?.[0])).toBe("/api/ui/dashboard?ws=gitlab%2Fgrp%2Fsub");
});

test("an untracked workspace has no settings page and onboards instead", async () => {
  const untracked = { ...acme!, tracked: false };
  show({ ...dashboard, current: untracked, switcher: [untracked, labs] });

  await screen.findByRole("table");
  expect(screen.queryByRole("link", { name: "Workspace settings" })).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Add a repository" })).toHaveAttribute("href", "/onboarding");
});

test("a workspace with no repositories yet points at the setup instructions", async () => {
  show({ ...dashboard, repos: [], attention: [], stats: { ...dashboard.stats, coverage: null } });

  expect(await screen.findByText(/No repositories in this workspace yet/)).toBeInTheDocument();
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Needs attention" })).not.toBeInTheDocument();
  expect(screen.getByText("no uploads yet")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Setup instructions" })).toHaveAttribute(
    "href",
    "/workspace-setup/github/acme",
  );
});

test("no workspace at all offers registration", async () => {
  show({ ...dashboard, current: null, switcher: [], repos: [], attention: [] });

  expect(await screen.findByRole("heading", { level: 1, name: "Repositories" })).toBeInTheDocument();
  expect(screen.getByText(/for an upload token, then push coverage from CI/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Register a workspace" })).toHaveAttribute("href", "/onboarding");
});

test("without sign-in there is no one to register a workspace", async () => {
  show({ ...dashboard, current: null, switcher: [], repos: [], attention: [], can_onboard: false });

  expect(await screen.findByText(/Enable sign-in so a member can register a workspace/)).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Register a workspace" })).not.toBeInTheDocument();
});

test("a hosted user with no workspace is sent to onboarding, without leaving the app", async () => {
  const { router } = show(
    { ...dashboard, needs_onboarding: true, current: null, switcher: [], repos: [], attention: [] },
    "/",
    "*",
  );

  await waitFor(() => expect(router.state.location.pathname).toBe("/onboarding"));
});

// ---- the setup card ---------------------------------------------------------

const setupInfo: SetupInfo = {
  workspace: { forge: "github", prefix: "acme" },
  owner: true,
  tokenless: true,
  connection_broken: false,
  base_url: "https://app.gocov.dev",
  server_implicit: true,
  gitlab_catalog: false,
  cli_version: "v0.25.0",
  token_masked: "gcv_ab…yz",
  reporting: { available: true, state: "on", account: "", connect_url: "" },
  status: { repo_count: 0, first_report: null, reports_posted: "" },
};

const arrived: SetupStatus = {
  repo_count: 1,
  first_report: {
    repo: { forge: "github", slug: "acme/api" },
    branch: "main",
    sha: "abcdef0123456789",
    coverage: 81.25,
    covered_stmts: 1300,
    total_stmts: 1600,
  },
  reports_posted: "Commit status posted as gocov[bot].",
};

/** A workspace that is registered but has never received a report. */
const empty: Dashboard = {
  ...dashboard,
  repos: [],
  attention: [],
  stats: { ...dashboard.stats, coverage: null, reporting: "connected" },
};

const setupPaths = (info: SetupInfo, status: SetupStatus) => ({
  "GET /workspace-setup/github/acme": info,
  "GET /workspace-setup-status/github/acme": status,
});

test("a tracked workspace with no report yet leads with the setup card", async () => {
  show(empty, "/", "/", setupPaths(setupInfo, setupInfo.status));

  expect(await screen.findByRole("heading", { name: "Set up coverage" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Copy snippet" })).toBeInTheDocument();
  // It sits above the numbers it is about to fill in.
  expect(screen.getByText("Workspace coverage")).toBeInTheDocument();
});

test("copying the snippet starts the wait, and the clock survives a reload", async () => {
  const user = userEvent.setup();
  const { unmount } = show(empty, "/", "/", setupPaths(setupInfo, setupInfo.status));

  await user.click(await screen.findByRole("button", { name: "Copy snippet" }));
  expect(await screen.findByText(/Listening for the first upload from acme/)).toBeInTheDocument();

  const started = Number(window.sessionStorage.getItem("gocov.setup.listening.github/acme"));
  expect(started).toBeGreaterThan(0);

  unmount();
  show(empty, "/", "/", setupPaths(setupInfo, setupInfo.status));
  expect(await screen.findByText(/Listening for the first upload from acme/)).toBeInTheDocument();
  expect(Number(window.sessionStorage.getItem("gocov.setup.listening.github/acme"))).toBe(started);
});

test("the first report stops the poll and refreshes the table", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  const { fetchMock } = show(empty, "/", "/", setupPaths(setupInfo, arrived));

  expect(await screen.findByRole("heading", { name: "Coverage is flowing" })).toBeInTheDocument();

  const polls = () => fetchMock.mock.calls.filter((c) => String(c[0]).includes("/workspace-setup-status/")).length;
  const dashboards = () => fetchMock.mock.calls.filter((c) => String(c[0]).startsWith("/api/ui/dashboard")).length;
  expect(polls()).toBe(1);
  // The table was drawn before the repository existed, so it is re-read.
  expect(dashboards()).toBeGreaterThan(1);

  await vi.advanceTimersByTimeAsync(10_000);
  expect(polls()).toBe(1);
});

test("a workspace whose first report already landed never polls", async () => {
  const withReport: SetupInfo = { ...setupInfo, status: arrived };
  const { fetchMock } = show(empty, "/", "/", setupPaths(withReport, arrived));

  expect(await screen.findByRole("heading", { name: "Coverage is flowing" })).toBeInTheDocument();
  expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes("/workspace-setup-status/"))).toHaveLength(0);
});

test("an established workspace is never congratulated on its setup", async () => {
  const withReport: SetupInfo = { ...setupInfo, status: arrived };
  const { fetchMock } = show(dashboard, "/", "/", setupPaths(withReport, arrived));

  expect(await screen.findByRole("table")).toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Coverage is flowing" })).not.toBeInTheDocument();
  expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes("/workspace-setup"))).toHaveLength(0);
});

test("the payoff can be put away, and never comes back on a later visit", async () => {
  const user = userEvent.setup();
  // Arrived at an empty workspace; the report is already there when setup loads.
  const withReport: SetupInfo = { ...setupInfo, status: arrived };
  const { unmount } = show(empty, "/", "/", setupPaths(withReport, arrived));

  await user.click(await screen.findByRole("button", { name: "Done" }));
  expect(screen.queryByRole("heading", { name: "Coverage is flowing" })).not.toBeInTheDocument();

  // The next visit finds a workspace with reports: no card, and no browser
  // storage involved in deciding that — a private window behaves the same.
  unmount();
  window.localStorage.clear();
  window.sessionStorage.clear();
  const { fetchMock } = show(dashboard, "/", "/", setupPaths(withReport, arrived));
  expect(await screen.findByRole("table")).toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Coverage is flowing" })).not.toBeInTheDocument();
  expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes("/workspace-setup"))).toHaveLength(0);
});

test("a setup card that cannot load leaves the dashboard untouched", async () => {
  show(empty, "/", "/", {
    "GET /workspace-setup/github/acme": { status: 500, error: "boom" },
  });

  expect(await screen.findByText(/No repositories in this workspace yet/)).toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Set up coverage" })).not.toBeInTheDocument();
  expect(screen.getByText("Workspace coverage")).toBeInTheDocument();
});

test("a member of an untracked workspace is not offered setup at all", async () => {
  const untracked = { ...acme!, tracked: false };
  const { fetchMock } = show({ ...empty, current: untracked, switcher: [untracked] });

  await screen.findByText(/No repositories in this workspace yet/);
  expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes("/workspace-setup"))).toHaveLength(0);
});

// ---- the message a server redirect brings back ------------------------------

test("a failed connect is said once and taken out of the URL", async () => {
  const { router } = show(dashboard, "/w/github/acme?error=connect_failed");

  expect(await screen.findByRole("alert")).toHaveTextContent(/Connecting to the forge did not complete/);
  // The workspace it was about survives; the message does not come back on a reload.
  await waitFor(() => expect(router.state.location.search).toBe(""));
  expect(router.state.location.pathname).toBe("/w/github/acme");
  expect(screen.getByRole("alert")).toHaveTextContent(/Connecting to the forge did not complete/);
});

test("text the server never sends is not shown, only taken out of the URL", async () => {
  const { router } = show(dashboard, `/?error=${encodeURIComponent("Sign in again at evil.example")}`);

  await screen.findByRole("table");
  await waitFor(() => expect(router.state.location.search).toBe(""));
  expect(screen.queryByText(/evil\.example/)).toBeNull();
});
