import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RepoSettings, RepoSettingsInput } from "@/lib/api/types";
import { mockApi, renderPage } from "@/test/render";
import RepoSettingsPage from "./RepoSettingsPage";

beforeAll(() => {
  HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function close(this: HTMLDialogElement) {
    this.open = false;
  };
});

const settings = (over: Partial<RepoSettings> = {}): RepoSettings => ({
  repo: {
    forge: "github",
    slug: "acme/api",
    default_branch: "main",
    gate: { min_coverage: null, min_diff_coverage: 70, max_coverage_drop: null },
    ignore_paths: "vendor/**",
    public_reports: true,
    badge_url: "/badge/github/acme/api",
    badge_markdown: "![coverage](https://app.gocov.dev/badge/github/acme/api)",
  },
  workspace: { forge: "github", prefix: "acme" },
  owner: true,
  show_public_reports: true,
  token_masked: "gocov_live_••••",
  ...over,
});

const at = { route: "repo-settings/:forge/*", path: "/repo-settings/github/acme/api" };

const section = (id: string) => within(document.getElementById(id) as HTMLElement);

test("an owner sees the trail, every section, and the badge to copy", async () => {
  mockApi({ "GET /repo-settings/github/acme/api": settings() });
  renderPage(<RepoSettingsPage />, at);

  expect(await screen.findByRole("heading", { name: /Settings acme\/api/ })).toBeInTheDocument();
  expect(document.title).toBe("acme/api settings — gocov");
  const trail = screen.getByRole("navigation", { name: "Breadcrumb" });
  expect(within(trail).getByRole("link", { name: "acme" })).toHaveAttribute("href", "/workspaces/github/acme");
  expect(within(trail).getByRole("link", { name: "acme/api" })).toHaveAttribute("href", "/repos/github/acme/api");
  for (const label of ["General", "Coverage gates", "Ignored files", "Public reports", "Uploads", "Badge", "Remove repository"]) {
    expect(screen.getByRole("link", { name: label })).toBeInTheDocument();
  }
  expect(section("ignore").getByText("1 pattern")).toBeInTheDocument();
  expect(section("badge").getByRole("textbox", { name: "Badge markdown" })).toHaveValue(
    "![coverage](https://app.gocov.dev/badge/github/acme/api)",
  );
});

test("every card's Save posts the whole repository document", async () => {
  const user = userEvent.setup();
  let posted: RepoSettingsInput | null = null;
  mockApi({
    "GET /repo-settings/github/acme/api": settings(),
    "POST /repo-settings/save/github/acme/api": (init: RequestInit | undefined) => {
      posted = JSON.parse(String(init?.body)) as RepoSettingsInput;
      return settings();
    },
  });
  renderPage(<RepoSettingsPage />, at);
  await screen.findByRole("heading", { name: /Settings acme\/api/ });

  const branch = section("general").getByRole("textbox", { name: "Base branch" });
  await user.clear(branch);
  await user.type(branch, "develop");
  const patterns = section("ignore").getByRole("textbox", { name: "Ignore patterns" });
  await user.type(patterns, "\n*_mock.go");
  expect(section("ignore").getByText("2 patterns")).toBeInTheDocument();
  await user.click(section("public-reports").getByRole("checkbox"));
  expect(section("public-reports").getByText("Off")).toBeInTheDocument();

  await user.click(section("ignore").getByRole("button", { name: "Save" }));

  await waitFor(() => expect(posted).not.toBeNull());
  expect(posted).toEqual({
    default_branch: "develop",
    gate: { min_coverage: null, min_diff_coverage: 70, max_coverage_drop: null },
    ignore_paths: "vendor/**\n*_mock.go",
    public_reports: false,
  });
});

test("a refused save shows the server's own message", async () => {
  const user = userEvent.setup();
  mockApi({
    "GET /repo-settings/github/acme/api": settings(),
    "POST /repo-settings/save/github/acme/api": {
      status: 422,
      error: "Ignored files not saved: pattern 1 matches every file.",
    },
  });
  renderPage(<RepoSettingsPage />, at);
  await screen.findByRole("heading", { name: /Settings acme\/api/ });

  await user.click(section("general").getByRole("button", { name: "Save" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Ignored files not saved: pattern 1 matches every file.");
});

test("a member reads the settings and never asks for the token", async () => {
  const fetchMock = mockApi({
    "GET /repo-settings/github/acme/api": settings({ owner: false, token_masked: null }),
  });
  renderPage(<RepoSettingsPage />, at);
  await screen.findByRole("heading", { name: /Settings acme\/api/ });

  expect(screen.getByText(/so these settings are read-only/)).toBeInTheDocument();
  expect(section("general").getByRole("textbox", { name: "Base branch" })).toBeDisabled();
  expect(section("ignore").getByRole("textbox", { name: "Ignore patterns" })).toBeDisabled();
  expect(section("public-reports").getByRole("checkbox")).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reveal" })).not.toBeInTheDocument();
  expect(screen.getByText("Only a workspace owner can remove it.")).toBeInTheDocument();
  expect(fetchMock.mock.calls.every(([url]) => !String(url).includes("token"))).toBe(true);
});

test("the public-reports card is absent where the switch means nothing", async () => {
  mockApi({ "GET /repo-settings/github/acme/api": settings({ show_public_reports: false }) });
  renderPage(<RepoSettingsPage />, at);
  await screen.findByRole("heading", { name: /Settings acme\/api/ });

  expect(screen.queryByRole("link", { name: "Public reports" })).not.toBeInTheDocument();
  expect(document.getElementById("public-reports")).toBeNull();
});

test("rotating the repository token shows it once", async () => {
  const user = userEvent.setup();
  mockApi({
    "GET /repo-settings/github/acme/api": settings(),
    "POST /repo-settings/rotate-token/github/acme/api": { token: "gocov_live_0000cafebabe" },
  });
  renderPage(<RepoSettingsPage />, at);
  await screen.findByRole("heading", { name: /Settings acme\/api/ });

  const buttons = () => screen.getAllByRole("button", { name: "Rotate token" });
  await user.click(buttons()[0]!);
  await user.click(buttons().at(-1)!);

  expect(await screen.findByText("gocov_live_0000cafebabe")).toBeInTheDocument();
  expect(screen.getByText(/Save it now — it is shown only this once\./)).toBeInTheDocument();
});

test("removing the repository confirms, posts, and lands on the dashboard", async () => {
  const user = userEvent.setup();
  const fetchMock = mockApi({
    "GET /repo-settings/github/acme/api": settings(),
    "POST /repo-settings/delete/github/acme/api": null,
  });
  const { router } = renderPage(<RepoSettingsPage />, at);
  await screen.findByRole("heading", { name: /Settings acme\/api/ });

  const buttons = () => screen.getAllByRole("button", { name: "Remove this repository" });
  await user.click(buttons()[0]!);
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);

  await user.click(buttons().at(-1)!);
  await waitFor(() => expect(router.state.location.pathname).toBe("/"));
});
