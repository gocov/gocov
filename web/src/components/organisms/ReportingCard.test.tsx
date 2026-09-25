import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import type { WorkspaceSettings } from "@/lib/api/types";
import { ReportingCard } from "./ReportingCard";

beforeAll(() => {
  HTMLDialogElement.prototype.showModal ??= function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function close(this: HTMLDialogElement) {
    this.open = false;
  };
});

type Reporting = WorkspaceSettings["reporting"];

const reporting = (r: Partial<Reporting> = {}): Reporting => ({
  available: true,
  state: "on",
  account: "",
  connect_url: "https://github.com/apps/gocov/installations/new",
  ...r,
});

function show(props: Partial<Parameters<typeof ReportingCard>[0]> = {}) {
  const onDisconnect = vi.fn();
  render(
    <MemoryRouter>
      <ReportingCard
        forge="github"
        reporting={reporting()}
        owner
        repoCount={8}
        onDisconnect={onDisconnect}
        {...props}
      />
    </MemoryRouter>,
  );
  return onDisconnect;
}

test("renders nothing where the deployment cannot connect this forge", () => {
  const { container } = render(
    <MemoryRouter>
      <ReportingCard
        forge="gitlab"
        reporting={reporting({ available: false, connect_url: "" })}
        owner
        repoCount={2}
        onDisconnect={() => {}}
      />
    </MemoryRouter>,
  );
  expect(container).toBeEmptyDOMElement();
});

test("a connected GitHub workspace posts as the bot and can be managed on GitHub", () => {
  show();
  expect(screen.getByText("Connected")).toBeInTheDocument();
  expect(screen.getByText("gocov[bot]")).toBeInTheDocument();
  expect(screen.getByText(/Posting through the app install · 8 repositories/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Manage on GitHub" })).toHaveAttribute(
    "href",
    "https://github.com/apps/gocov/installations/new",
  );
});

test("a disconnected GitLab workspace is offered the grant, in GitLab's words", () => {
  show({
    forge: "gitlab",
    reporting: reporting({ state: "off", connect_url: "/workspace-settings/gitlab/acme/connect" }),
  });
  expect(screen.getByText("Not connected")).toBeInTheDocument();
  expect(screen.getByText(/commit statuses and merge-request comments/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Grant write access" })).toHaveAttribute(
    "href",
    "/workspace-settings/gitlab/acme/connect",
  );
  expect(screen.queryByRole("button", { name: "Disconnect" })).not.toBeInTheDocument();
});

test("a broken Bitbucket grant explains itself and offers it again", () => {
  show({
    forge: "bitbucket",
    reporting: reporting({ state: "broken", account: "omer", connect_url: "/workspace-settings/bitbucket/acme/connect" }),
  });
  expect(screen.getByText("Reconnect needed")).toBeInTheDocument();
  expect(screen.getByRole("alert")).toHaveTextContent("The grant stopped working");
  expect(screen.getByText("@omer")).toBeInTheDocument();
  expect(screen.getByText("Grant revoked")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Grant write access again" })).toBeInTheDocument();
});

test("a broken GitHub install is called an install, not a grant", () => {
  show({
    forge: "github",
    reporting: reporting({ state: "broken", account: "", connect_url: "https://github.com/apps/gocov/installations/new" }),
  });
  expect(screen.getByRole("alert")).toHaveTextContent("The app install stopped working — it was removed or suspended on GitHub.");
  expect(screen.getByRole("alert")).not.toHaveTextContent("grant");
  expect(screen.getByRole("link", { name: "Reinstall the app" })).toBeInTheDocument();
});

test("disconnecting is gated by the confirmation", async () => {
  const user = userEvent.setup();
  const onDisconnect = show();

  const buttons = () => screen.getAllByRole("button", { name: "Disconnect" });
  await user.click(buttons()[0]!);
  expect(onDisconnect).not.toHaveBeenCalled();
  expect(screen.getByText("Statuses and comments are then skipped for this workspace.")).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(onDisconnect).not.toHaveBeenCalled();

  await user.click(buttons()[0]!);
  await user.click(buttons().at(-1)!);
  expect(onDisconnect).toHaveBeenCalledOnce();
});

test("a member is told whose move this is and is offered none", () => {
  show({ owner: false });
  expect(screen.getByText("Connecting and disconnecting are a workspace owner’s moves.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Disconnect" })).not.toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Manage on GitHub" })).not.toBeInTheDocument();
});
