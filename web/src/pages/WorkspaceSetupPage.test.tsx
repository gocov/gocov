import { screen } from "@testing-library/react";
import type { SetupInfo, SetupStatus } from "@/lib/api/types";
import { mockApi, renderPage } from "@/test/render";
import WorkspaceSetupPage from "./WorkspaceSetupPage";

afterEach(() => vi.unstubAllGlobals());
beforeEach(() => window.localStorage.clear());

const info: SetupInfo = {
  workspace: { forge: "github", prefix: "acme", forge_label: "GitHub" },
  owner: true,
  tokenless: false,
  connection_broken: false,
  base_url: "https://app.gocov.dev",
  server_implicit: true,
  gitlab_catalog: false,
  cli_version: "v0.25.0",
  token_masked: "gcv_ab…yz",
  reporting: { available: true, state: "off", account: "", connect_url: "" },
  status: { repo_count: 2, first_report: null, reports_posted: "" },
};

const show = (over: Partial<SetupInfo> = {}, status?: SetupStatus) => {
  const fetchMock = mockApi({
    "GET /workspace-setup/github/acme": { ...info, ...over },
    "GET /workspace-setup-status/github/acme": status ?? { ...info.status, ...over.status },
  });
  return {
    fetchMock,
    ...renderPage(<WorkspaceSetupPage />, {
      route: "workspace-setup/:forge/*",
      path: "/workspace-setup/github/acme",
    }),
  };
};

test("the page is the snippet, with the trail back to the workspace", async () => {
  show();

  expect(await screen.findByRole("heading", { level: 1, name: "Add a repository" })).toBeInTheDocument();
  expect(document.title).toBe("add a repository — gocov");
  expect(screen.getByRole("link", { name: "Repositories" })).toHaveAttribute("href", "/");
  expect(screen.getByRole("link", { name: "acme" })).toHaveAttribute("href", "/w/github/acme");
  expect(screen.getByRole("button", { name: "Copy snippet" })).toBeInTheDocument();
  expect(screen.getByText(".github/workflows/ci.yml")).toBeInTheDocument();
  expect(screen.getByText(/Listening for an upload from acme/)).toBeInTheDocument();
});

test("a repository that registers while the page is open says so", async () => {
  show({}, { repo_count: 3, first_report: null, reports_posted: "" });

  expect(await screen.findByText("New repository received.")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Open the dashboard" })).toHaveAttribute("href", "/w/github/acme");
  expect(screen.queryByText(/Listening for an upload/)).not.toBeInTheDocument();
});

test("a member without the token still gets the snippet", async () => {
  show({ owner: false, token_masked: null });

  expect(await screen.findByRole("button", { name: "Copy snippet" })).toBeInTheDocument();
  expect(screen.getByText("Shown to workspace owners only — ask one for it.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reveal" })).not.toBeInTheDocument();
});

test("a workspace the viewer cannot see answers with the not-found panel", async () => {
  mockApi({ "GET /workspace-setup/github/acme": { status: 404, error: "not found" } });
  renderPage(<WorkspaceSetupPage />, {
    route: "workspace-setup/:forge/*",
    path: "/workspace-setup/github/acme",
  });

  expect(await screen.findByText(/We couldn’t find that page/)).toBeInTheDocument();
});
