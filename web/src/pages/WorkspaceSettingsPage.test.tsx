import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WorkspaceSettings, WorkspaceSettingsInput } from "@/lib/api/types";
import { mockApi, renderPage } from "@/test/render";
import WorkspaceSettingsPage from "./WorkspaceSettingsPage";

beforeAll(() => {
  HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function close(this: HTMLDialogElement) {
    this.open = false;
  };
});

const settings = (over: Partial<WorkspaceSettings> = {}): WorkspaceSettings => ({
  workspace: {
    forge: "github",
    prefix: "acme",
    forge_label: "GitHub",
    default_branch: "main",
    report_retention_days: 90,
    gate: { min_coverage: 80, min_diff_coverage: null, max_coverage_drop: null },
  },
  owner: true,
  repo_count: 8,
  reporting: { available: true, state: "on", account: "", connect_url: "https://github.com/apps/gocov" },
  server_url: null,
  token_masked: "gocov_live_••••",
  ...over,
});

const at = { route: "workspaces/:forge/:prefix", path: "/workspaces/github/acme" };

const section = (id: string) => within(document.getElementById(id) as HTMLElement);

test("an owner sees every section and the workspace it belongs to", async () => {
  mockApi({ "GET /workspaces/github/acme": settings() });
  renderPage(<WorkspaceSettingsPage />, at);

  expect(await screen.findByRole("heading", { name: /Workspace acme/ })).toBeInTheDocument();
  expect(document.title).toBe("acme settings — gocov");
  expect(screen.getByText(/GitHub · 8 repositories/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Setup instructions" })).toHaveAttribute(
    "href",
    "/workspaces/github/acme/setup",
  );
  for (const label of ["Reporting", "Uploads", "Coverage gates", "Defaults", "Delete workspace"]) {
    expect(screen.getByRole("link", { name: label })).toBeInTheDocument();
  }
});

test("gates and defaults are one form: either Save posts all of it", async () => {
  const user = userEvent.setup();
  let posted: WorkspaceSettingsInput | null = null;
  const saved = settings({
    workspace: { ...settings().workspace, default_branch: "trunk", report_retention_days: 365 },
  });
  mockApi({
    "GET /workspaces/github/acme": settings(),
    "POST /workspaces/github/acme/settings": (init: RequestInit | undefined) => {
      posted = JSON.parse(String(init?.body)) as WorkspaceSettingsInput;
      return saved;
    },
  });
  renderPage(<WorkspaceSettingsPage />, at);
  await screen.findByRole("heading", { name: /Workspace acme/ });

  const branch = section("defaults").getByRole("textbox", { name: "Default branch" });
  await user.clear(branch);
  await user.type(branch, "trunk");
  await user.selectOptions(section("defaults").getByRole("combobox", { name: "Keep reports for" }), "365");
  await user.click(section("gates").getByRole("switch", { name: "Minimum diff coverage" }));

  // Pressing the gates card's Save carries the Defaults edits with it.
  await user.click(section("gates").getByRole("button", { name: "Save" }));

  await waitFor(() => expect(posted).not.toBeNull());
  expect(posted).toEqual({
    default_branch: "trunk",
    report_retention_days: 365,
    gate: { min_coverage: 80, min_diff_coverage: 70, max_coverage_drop: null },
  });
  expect(await section("gates").findByText("Saved")).toBeInTheDocument();
});

test("a rejected save shows the server's own message", async () => {
  const user = userEvent.setup();
  mockApi({
    "GET /workspaces/github/acme": settings(),
    "POST /workspaces/github/acme/settings": { status: 422, error: "Default branch cannot be empty." },
  });
  renderPage(<WorkspaceSettingsPage />, at);
  await screen.findByRole("heading", { name: /Workspace acme/ });

  await user.clear(section("defaults").getByRole("textbox", { name: "Default branch" }));
  await user.click(section("defaults").getByRole("button", { name: "Save" }));

  expect(await screen.findByRole("alert")).toHaveTextContent("Default branch cannot be empty.");
});

test("a member sees read-only settings and never asks for the token", async () => {
  const fetchMock = mockApi({
    "GET /workspaces/github/acme": settings({ owner: false, token_masked: null }),
  });
  renderPage(<WorkspaceSettingsPage />, at);
  await screen.findByRole("heading", { name: /Workspace acme/ });

  expect(screen.getByText(/so these settings are read-only/)).toBeInTheDocument();
  expect(section("defaults").getByRole("textbox", { name: "Default branch" })).toBeDisabled();
  expect(section("defaults").getByRole("combobox", { name: "Keep reports for" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
  expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reveal" })).not.toBeInTheDocument();
  expect(screen.getByText("Only a workspace owner can delete it.")).toBeInTheDocument();
  expect(fetchMock.mock.calls.every(([url]) => !String(url).includes("token"))).toBe(true);
});

test("disconnecting reporting posts once confirmed and re-reads the answer", async () => {
  const user = userEvent.setup();
  mockApi({
    "GET /workspaces/github/acme": settings(),
    "POST /workspaces/github/acme/disconnect": settings({
      reporting: { available: true, state: "off", account: "", connect_url: "https://github.com/apps/gocov" },
    }),
  });
  renderPage(<WorkspaceSettingsPage />, at);
  await screen.findByRole("heading", { name: /Workspace acme/ });

  const buttons = () => screen.getAllByRole("button", { name: "Disconnect" });
  await user.click(buttons()[0]!);
  await user.click(buttons().at(-1)!);

  expect(await screen.findByText("Not connected")).toBeInTheDocument();
});

test("deleting confirms, posts, and lands back on the dashboard", async () => {
  const user = userEvent.setup();
  const fetchMock = mockApi({
    "GET /workspaces/github/acme": settings(),
    "POST /workspaces/github/acme/delete": null,
  });
  const { router } = renderPage(<WorkspaceSettingsPage />, at);
  await screen.findByRole("heading", { name: /Workspace acme/ });

  const buttons = () => screen.getAllByRole("button", { name: "Delete this workspace" });
  await user.click(buttons()[0]!);
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);

  await user.click(buttons().at(-1)!);
  await waitFor(() => expect(router.state.location.pathname).toBe("/"));
  expect(
    fetchMock.mock.calls.some(([url, init]) => init?.method === "POST" && String(url).endsWith("/delete")),
  ).toBe(true);
});

test("the message a grant redirect carries is shown once, then left out of the URL", async () => {
  mockApi({ "GET /workspaces/github/acme": settings() });
  const { router } = renderPage(<WorkspaceSettingsPage />, {
    ...at,
    path: "/workspaces/github/acme?notice=Workspace+connected.",
  });

  expect(await screen.findByText("Workspace connected.")).toBeInTheDocument();
  await waitFor(() => expect(router.state.location.search).toBe(""));
  expect(screen.getByText("Workspace connected.")).toBeInTheDocument();
});

test("an error from the same redirect reads as a failure", async () => {
  mockApi({ "GET /workspaces/github/acme": settings() });
  renderPage(<WorkspaceSettingsPage />, {
    ...at,
    path: "/workspaces/github/acme?error=The+connection+could+not+be+completed.",
  });

  expect(await screen.findByRole("alert")).toHaveTextContent("The connection could not be completed.");
});
