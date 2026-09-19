import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import type { OnboardingInfo, OnboardingRow } from "@/lib/api/types";
import { WorkspacePicker } from "./WorkspacePicker";

const info = (over: Partial<OnboardingInfo> = {}): OnboardingInfo => ({
  forge: "bitbucket",
  forge_label: "Bitbucket",
  account: "Ada Lovelace",
  mode: "pick",
  install_url: "",
  rows: [],
  membership_count: 0,
  ...over,
});

/** Deliberately out of order: the picker sorts them. */
const rows: OnboardingRow[] = [
  { prefix: "other-co", state: "unowned" },
  { prefix: "mine", state: "member" },
  { prefix: "joinable", state: "registered" },
  { prefix: "acme", state: "available" },
];

function show(props: Partial<Parameters<typeof WorkspacePicker>[0]> = {}) {
  const onRegister = vi.fn();
  const onEvent = vi.fn();
  render(
    <MemoryRouter>
      <WorkspacePicker info={info()} busy={null} onRegister={onRegister} onEvent={onEvent} {...props} />
    </MemoryRouter>,
  );
  return { onRegister, onEvent };
}

test("GitHub is offered the app install, and nothing else", async () => {
  const user = userEvent.setup();
  const { onEvent } = show({
    info: info({ forge: "github", forge_label: "GitHub", mode: "install", install_url: "https://github.com/apps/gocov/installations/new" }),
  });

  expect(screen.getByRole("heading", { name: "Install gocov on your GitHub organization" })).toBeInTheDocument();
  expect(screen.getByText(/GitHub’s approval screen is where you pick the organization/)).toBeInTheDocument();
  const install = screen.getByRole("link", { name: "Install the gocov app" });
  expect(install).toHaveAttribute("href", "https://github.com/apps/gocov/installations/new");
  expect(install).not.toHaveAttribute("target");
  expect(screen.getByText(/gocov never needs your source code/)).toBeInTheDocument();

  // jsdom cannot follow the forge URL; the click still has to reach the handler.
  install.addEventListener("click", (e) => e.preventDefault());
  await user.click(install);
  expect(onEvent).toHaveBeenCalledWith("install_app_clicked", { forge: "github" });
});

test("an instance without the GitHub App says so instead of offering a dead link", () => {
  show({ info: info({ forge: "github", forge_label: "GitHub", mode: "install", install_url: "" }) });
  expect(screen.getByText(/The GitHub App is not configured on this instance/)).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Install the gocov app" })).not.toBeInTheDocument();
});

test("every membership gets one action or the reason there is none, free ones first", () => {
  show({ info: info({ rows, membership_count: 4 }) });

  expect(screen.getByRole("heading", { name: "Choose a workspace" })).toBeInTheDocument();
  expect(screen.getAllByText(/^(acme|joinable|mine|other-co)$/).map((el) => el.textContent)).toEqual([
    "acme",
    "joinable",
    "mine",
    "other-co",
  ]);

  expect(screen.getByRole("button", { name: "Create" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Join" })).toBeEnabled();
  expect(screen.getByRole("link", { name: "Open" })).toHaveAttribute("href", "/?ws=bitbucket%2Fmine");
  expect(screen.getByText("Creating it takes an owner of the workspace — ask one to sign in and set it up.")).toBeInTheDocument();
  // unowned carries no control of its own.
  expect(screen.getAllByRole("button")).toHaveLength(2);
});

test("Create and Join hand the prefix back", async () => {
  const user = userEvent.setup();
  const { onRegister, onEvent } = show({ info: info({ rows, membership_count: 4 }) });

  await user.click(screen.getByRole("button", { name: "Create" }));
  expect(onRegister).toHaveBeenCalledWith("acme");
  expect(onEvent).toHaveBeenCalledWith("register_workspace_clicked", { forge: "bitbucket" });

  await user.click(screen.getByRole("button", { name: "Join" }));
  expect(onRegister).toHaveBeenCalledWith("joinable");
});

test("the row being registered spins, and the rest wait with it", () => {
  show({ info: info({ rows, membership_count: 4 }), busy: "acme" });
  expect(screen.getByRole("button", { name: "Creating" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Join" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Create" })).not.toBeInTheDocument();
});

test("on GitLab they are groups", () => {
  show({
    info: info({ forge: "gitlab", forge_label: "GitLab", rows: [{ prefix: "other-co", state: "unowned" }], membership_count: 1 }),
  });
  expect(screen.getByRole("heading", { name: "Choose a group" })).toBeInTheDocument();
  expect(screen.getByText("Creating it takes an owner of the group — ask one to sign in and set it up.")).toBeInTheDocument();
  expect(screen.getByText(/1 membership read at sign-in/)).toBeInTheDocument();
});

test("no memberships at all says where the list came from", () => {
  show({ info: info({ rows: [], membership_count: 0 }) });
  expect(screen.getByText("Your Bitbucket account reported no workspaces at sign-in.")).toBeInTheDocument();
  expect(screen.getByText(/Signed in as Ada Lovelace/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Sign in again" })).toHaveAttribute(
    "href",
    "/oauth/bitbucket/start?next=%2Fonboarding",
  );
});
