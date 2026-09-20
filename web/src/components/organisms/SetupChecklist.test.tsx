import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SetupInfo, SetupStatus } from "@/lib/api/types";
import { renderPage } from "@/test/render";
import { SetupChecklist } from "./SetupChecklist";

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

const waiting: SetupStatus = { repo_count: 0, first_report: null, reports_posted: "" };

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

const onReveal = vi.fn(async () => "gcv_the_whole_token");
const onCopied = vi.fn();
const onDismiss = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
});

const show = (over: { info?: Partial<SetupInfo>; status?: SetupStatus; listeningSince?: number | null } = {}) =>
  renderPage(
    <SetupChecklist
      info={{ ...info, ...over.info }}
      status={over.status ?? waiting}
      listeningSince={over.listeningSince ?? null}
      onReveal={onReveal}
      onCopied={onCopied}
      onDismiss={onDismiss}
    />,
  );

test("before the snippet is copied, the card is the snippet and a line about what happens next", () => {
  show();

  // The workspace is the server's state, not a step: it sits in the header.
  expect(screen.getByText("Workspace acme ready")).toBeInTheDocument();
  expect(screen.getByText(".github/workflows/ci.yml")).toBeInTheDocument();
  expect(screen.getByText(/The next pipeline run that reaches the upload step/)).toBeInTheDocument();

  expect(screen.queryByRole("status")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Done" })).not.toBeInTheDocument();
});

test("copying the snippet reports it and, once listening, collapses the card", async () => {
  const user = userEvent.setup();
  const { rerender } = show();

  await user.click(screen.getByRole("button", { name: "Copy snippet" }));
  expect(onCopied).toHaveBeenCalledTimes(1);

  // The page owns the clock: it hands the card back a listening timestamp.
  rerender(
    <SetupChecklist
      info={info}
      status={waiting}
      listeningSince={Date.now()}
      onReveal={onReveal}
      onCopied={onCopied}
      onDismiss={onDismiss}
    />,
  );

  expect(screen.getByText(/Snippet copied/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Copy snippet" })).not.toBeInTheDocument();
  expect(screen.queryByText(/The next pipeline run that reaches the upload step/)).not.toBeInTheDocument();
  // The wait is the card's own footer, and it announces itself.
  expect(screen.getByRole("status")).toHaveTextContent("Listening for the first upload from acme…");
  expect(screen.getByText("Workspace acme ready")).toBeInTheDocument();
});

test("the snippet comes back on demand after it has been copied", async () => {
  const user = userEvent.setup();
  show({ listeningSince: Date.now() });

  await user.click(screen.getByRole("button", { name: "Show snippet" }));
  expect(screen.getByRole("button", { name: "Copy snippet" })).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Hide snippet" }));
  expect(screen.queryByRole("button", { name: "Copy snippet" })).not.toBeInTheDocument();
});

test("silence is only explained after twenty seconds of it", () => {
  show({ listeningSince: Date.now() - 19_000 });
  expect(screen.queryByText(/Nothing yet/)).not.toBeInTheDocument();
});

test("after twenty seconds the card offers the three things to check, and permission to leave", () => {
  show({ listeningSince: Date.now() - 21_000 });

  expect(screen.getByText(/Nothing yet — that is normal while a pipeline runs/)).toBeInTheDocument();
  const items = screen.getAllByRole("listitem").filter((li) => li.textContent?.startsWith("The "));
  expect(items).toHaveLength(3);
  expect(items[0]).toHaveTextContent("id-token: write");
  expect(screen.getByRole("link", { name: "The GitHub recipe in the docs" })).toHaveAttribute(
    "href",
    "https://docs.gocov.dev/github-actions/",
  );
  expect(screen.getByText(/You can leave this page/)).toBeInTheDocument();
});

test("the help is the token-mode help when the workspace uploads with a token", () => {
  show({ info: { tokenless: false }, listeningSince: Date.now() - 30_000 });
  expect(screen.getByText(/The secret is named exactly GOCOV_TOKEN/)).toBeInTheDocument();
});

test("a GitLab workspace gets GitLab's help and GitLab's recipe", () => {
  show({
    info: { workspace: { forge: "gitlab", prefix: "acme", forge_label: "GitLab" }, tokenless: false },
    listeningSince: Date.now() - 30_000,
  });
  expect(screen.getByText(/masked but not protected/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "The GitLab recipe in the docs" })).toHaveAttribute(
    "href",
    "https://docs.gocov.dev/gitlab-ci/",
  );
});

test("the first report turns the card into the payoff", async () => {
  const user = userEvent.setup();
  show({ status: arrived, listeningSince: Date.now() - 30_000 });

  expect(screen.getByRole("heading", { name: "Coverage is flowing" })).toBeInTheDocument();
  expect(screen.getByText("81.3%")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "acme/api" })).toHaveAttribute("href", "/repos/github/acme/api");
  expect(screen.getByText("main")).toBeInTheDocument();
  expect(screen.getByText("abcdef012345")).toBeInTheDocument();
  expect(screen.getByText("Commit status posted as gocov[bot].")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Set a coverage gate" })).toHaveAttribute("href", "/workspace-settings/github/acme");
  // Reporting is already on: nothing to offer.
  expect(screen.queryByRole("link", { name: "Turn on reporting" })).not.toBeInTheDocument();
  expect(screen.queryByText(/Listening for the first upload/)).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Done" }));
  expect(onDismiss).toHaveBeenCalledTimes(1);
});

test("with reporting off, the payoff says so and offers to turn it on", () => {
  show({
    info: { reporting: { available: true, state: "off", account: "", connect_url: "/github/install" } },
    status: { ...arrived, reports_posted: "" },
    listeningSince: Date.now() - 30_000,
  });

  expect(screen.getByText("Nothing was posted back to GitHub yet.")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Turn on reporting" })).toHaveAttribute("href", "/workspace-settings/github/acme");
});

test("where the deployment cannot report at all, the payoff stays quiet about it", () => {
  show({
    info: { reporting: { available: false, state: "off", account: "", connect_url: "" } },
    status: { ...arrived, reports_posted: "" },
    listeningSince: Date.now() - 30_000,
  });

  expect(screen.queryByText(/Nothing was posted back/)).not.toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Turn on reporting" })).not.toBeInTheDocument();
});
