import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SettingsLayout } from "./SettingsLayout";

const nav = [
  { id: "uploads", label: "Uploads", group: "Workspace" },
  { id: "gates", label: "Coverage gates", group: "Workspace" },
  { id: "delete", label: "Delete workspace", group: "Danger zone" },
];

function Page() {
  return (
    <SettingsLayout header={<h1>Workspace acme</h1>} nav={nav}>
      <SettingsLayout.Section id="uploads">Uploads card</SettingsLayout.Section>
      <SettingsLayout.Section id="gates">Gates card</SettingsLayout.Section>
      <SettingsLayout.Section id="delete">Delete card</SettingsLayout.Section>
    </SettingsLayout>
  );
}

test("lists the sections under their groups and points each link at its card", () => {
  render(<Page />);
  const sidebar = screen.getByRole("navigation", { name: "Settings sections" });
  expect(within(sidebar).getByText("Workspace")).toBeInTheDocument();
  expect(within(sidebar).getByText("Danger zone")).toBeInTheDocument();
  expect(within(sidebar).getByRole("link", { name: "Coverage gates" })).toHaveAttribute("href", "#gates");
  expect(document.getElementById("gates")).toHaveTextContent("Gates card");
});

test("the first section is current until another is clicked", async () => {
  const user = userEvent.setup();
  render(<Page />);
  expect(screen.getByRole("link", { name: "Uploads" })).toHaveAttribute("aria-current", "true");

  await user.click(screen.getByRole("link", { name: "Delete workspace" }));
  expect(screen.getByRole("link", { name: "Delete workspace" })).toHaveAttribute("aria-current", "true");
  expect(screen.getByRole("link", { name: "Uploads" })).not.toHaveAttribute("aria-current");
});

test("renders without an IntersectionObserver (jsdom has none)", () => {
  expect(typeof IntersectionObserver).toBe("undefined");
  render(<Page />);
  expect(screen.getByRole("heading", { name: "Workspace acme" })).toBeInTheDocument();
});
