import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderPage } from "@/test/render";
import ComponentsPage from "./ComponentsPage";

test("the gallery shows both layers and its live specimens work", async () => {
  renderPage(<ComponentsPage />, { route: "_components", path: "/_components" });

  expect(screen.getByRole("heading", { level: 1, name: "Components" })).toBeInTheDocument();
  expect(document.title).toBe("components — gocov");
  expect(screen.getByRole("heading", { level: 2, name: "Atoms" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { level: 2, name: "Molecules" })).toBeInTheDocument();

  // The interactive specimens are controlled by the page, not frozen props.
  const filters = screen.getByRole("group", { name: "Filter files" });
  const changed = within(filters).getByRole("button", { name: /^Changed/ });
  await userEvent.click(changed);
  expect(changed).toHaveAttribute("aria-pressed", "true");
});
