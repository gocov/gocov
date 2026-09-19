import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WorkspaceGroup } from "@/lib/api/types";
import { renderPage } from "@/test/render";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";

const group = (over: Partial<WorkspaceGroup> = {}): WorkspaceGroup => ({
  forge: "github",
  prefix: "acme",
  forge_name: "GitHub",
  repo_count: 4,
  coverage: 74.6,
  current: false,
  tracked: true,
  ...over,
});

const acme = group({ current: true });
const labs = group({ forge: "gitlab", prefix: "acme-labs", forge_name: "GitLab", coverage: null });

test("a single workspace is a plain title", () => {
  renderPage(<WorkspaceSwitcher current={acme} groups={[acme]} canOnboard />);
  expect(screen.getByText("acme")).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});

test("the title opens a list of the other workspaces", async () => {
  renderPage(<WorkspaceSwitcher current={acme} groups={[acme, labs]} canOnboard />);

  const trigger = screen.getByRole("button", { name: /acme/ });
  expect(trigger).toHaveTextContent("GitHub");
  expect(trigger).toHaveAttribute("aria-expanded", "false");
  expect(trigger).toHaveAttribute("aria-haspopup", "true");
  expect(screen.queryByRole("link", { name: /acme-labs/ })).not.toBeInTheDocument();

  await userEvent.click(trigger);
  expect(trigger).toHaveAttribute("aria-expanded", "true");

  const current = screen.getByRole("link", { name: /^acme74\.6%$/ });
  expect(current).toHaveAttribute("aria-current", "true");
  expect(current).toHaveAttribute("href", "/?ws=github%2Facme");

  const other = screen.getByRole("link", { name: /acme-labs/ });
  expect(other).not.toHaveAttribute("aria-current");
  expect(other).toHaveAttribute("href", "/?ws=gitlab%2Facme-labs");
  // No coverage yet reads as a dash, not 0%.
  expect(other).toHaveTextContent("—");

  expect(screen.getByRole("link", { name: "Connect a workspace" })).toHaveAttribute("href", "/onboarding");
});

test("the footer link is only for someone who may register one", async () => {
  renderPage(<WorkspaceSwitcher current={acme} groups={[acme, labs]} />);
  await userEvent.click(screen.getByRole("button", { name: /acme/ }));
  expect(screen.queryByRole("link", { name: "Connect a workspace" })).not.toBeInTheDocument();
});

test("Escape and a click outside close it", async () => {
  renderPage(<WorkspaceSwitcher current={acme} groups={[acme, labs]} />);
  const trigger = screen.getByRole("button", { name: /acme/ });

  await userEvent.click(trigger);
  await userEvent.keyboard("{Escape}");
  expect(trigger).toHaveAttribute("aria-expanded", "false");

  await userEvent.click(trigger);
  expect(trigger).toHaveAttribute("aria-expanded", "true");
  await userEvent.click(document.body);
  expect(trigger).toHaveAttribute("aria-expanded", "false");
  expect(screen.queryByRole("link", { name: /acme-labs/ })).not.toBeInTheDocument();
});

test("more than seven workspaces are searchable", async () => {
  const many = [acme, labs, ...Array.from({ length: 6 }, (_, i) => group({ prefix: `team-${i}` }))];
  renderPage(<WorkspaceSwitcher current={acme} groups={many} />);
  await userEvent.click(screen.getByRole("button", { name: /acme/ }));

  const search = screen.getByRole("searchbox", { name: "Find a workspace" });
  await userEvent.type(search, "labs");
  expect(screen.getByRole("link", { name: /acme-labs/ })).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: /team-0/ })).not.toBeInTheDocument();
});

test("seven or fewer are listed without a search box", async () => {
  renderPage(<WorkspaceSwitcher current={acme} groups={[acme, labs]} />);
  await userEvent.click(screen.getByRole("button", { name: /acme/ }));
  expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
});
