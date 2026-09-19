import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SetupInfo } from "@/lib/api/types";
import { renderPage } from "@/test/render";
import { SnippetPanel } from "./SnippetPanel";

const info: SetupInfo = {
  workspace: { forge: "github", prefix: "acme", forge_label: "GitHub" },
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

const reveal = vi.fn(async () => "gcv_the_whole_token");

beforeEach(() => {
  reveal.mockClear();
  window.localStorage.clear();
});

const show = (overrides: Partial<SetupInfo> = {}, onCopied?: () => void) =>
  renderPage(<SnippetPanel info={{ ...info, ...overrides }} onReveal={reveal} onCopied={onCopied} />);

const snippetText = () => screen.getByRole("group", { name: /snippet$/ }).textContent ?? "";

test("a connected workspace leads with the tokenless snippet and no secret", () => {
  show();
  expect(screen.getByText(/No secret needed/)).toHaveTextContent("short-lived identity token from GitHub");
  expect(screen.getByText(".github/workflows/ci.yml")).toBeInTheDocument();
  expect(snippetText()).toContain("id-token: write");
  expect(snippetText()).toContain("go test ./...");
  expect(screen.queryByText("GOCOV_SERVER")).not.toBeInTheDocument();
  expect(screen.getByText("Commit, branch, repository and pull request are detected automatically.")).toBeInTheDocument();
});

test("picking a language rewrites the snippet and is remembered", async () => {
  const user = userEvent.setup();
  const { unmount } = show();

  await user.click(within(screen.getByRole("group", { name: "Language" })).getByRole("button", { name: "Python" }));
  expect(snippetText()).toContain("pytest --cov --cov-report=xml");
  expect(snippetText()).toContain("files: coverage.xml");
  expect(snippetText()).not.toContain("go test");

  unmount();
  show();
  expect(snippetText()).toContain("pytest --cov --cov-report=xml");
});

test("Copy snippet is the one primary action, and reports the copy", async () => {
  const user = userEvent.setup();
  const onCopied = vi.fn();
  show({}, onCopied);

  const copy = screen.getByRole("button", { name: "Copy snippet" });
  await user.click(copy);

  expect(onCopied).toHaveBeenCalledTimes(1);
  expect(await window.navigator.clipboard.readText()).toContain("gocov/gocov-action@v1");
  expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument();
});

test("“Use a token instead” swaps the snippet and reveals the token", async () => {
  const user = userEvent.setup();
  show();

  // The token path exists but is folded away: it is not on the critical path.
  const disclosure = screen.getByText("Use a token instead").closest("details");
  expect(disclosure).not.toHaveAttribute("open");

  await user.click(screen.getByText("Use a token instead"));
  expect(disclosure).toHaveAttribute("open");

  expect(snippetText()).toContain("token: ${{ secrets.GOCOV_TOKEN }}");
  expect(snippetText()).not.toContain("id-token: write");
  expect(screen.getByText(/Secrets and variables/)).toBeInTheDocument();
  expect(screen.getByText("gcv_ab…yz")).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Reveal" }));
  expect(reveal).toHaveBeenCalledTimes(1);
  expect(await screen.findByText("gcv_the_whole_token")).toBeInTheDocument();

  // Closing it puts the identity-token snippet back.
  await user.click(screen.getByText("Use a token instead"));
  expect(snippetText()).toContain("id-token: write");
});

test("a token workspace shows where the secret goes, up front", () => {
  show({ tokenless: false });
  expect(screen.getByText(/Secrets and variables/)).toBeInTheDocument();
  expect(screen.queryByText("Use a token instead")).not.toBeInTheDocument();
  expect(snippetText()).toContain("token: ${{ secrets.GOCOV_TOKEN }}");
});

test("a member who may not see the token is told who has it", () => {
  show({ tokenless: false, owner: false, token_masked: null });
  expect(screen.getByText("Shown to workspace owners only — ask one for it.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reveal" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Copy" })).not.toBeInTheDocument();
  // The snippet is still the whole point of the page.
  expect(snippetText()).toContain("gocov/gocov-action@v1");
});

test("a broken connection explains why the token is back, and links to the fix", () => {
  show({ tokenless: false, connection_broken: true });
  expect(screen.getByText(/no longer works/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Reconnect it" })).toHaveAttribute("href", "/workspaces/github/acme");
});

test("a self-hosted instance also hands over GOCOV_SERVER", () => {
  show({ server_implicit: false, base_url: "https://cov.acme.dev" });
  expect(screen.getByText("GOCOV_SERVER")).toBeInTheDocument();
  expect(screen.getByText("https://cov.acme.dev")).toBeInTheDocument();
  expect(snippetText()).toContain("server: ${{ vars.GOCOV_SERVER }}");
});

test("the GitLab panel offers the catalog component and its own filename", () => {
  show({ workspace: { forge: "gitlab", prefix: "acme/team", forge_label: "GitLab" }, gitlab_catalog: true });
  expect(screen.getByText(".gitlab-ci.yml")).toBeInTheDocument();
  expect(snippetText()).toContain("gitlab.com/gocov/gocov/upload@1");
});
