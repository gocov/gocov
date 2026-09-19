import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Banner } from "./Banner";

beforeEach(() => sessionStorage.clear());

test("dismissing it keeps it away for the rest of the session", async () => {
  const message = "Sign-in is off on this instance: everyone sees every report.";
  const { unmount } = render(
    <Banner tone="warn" id="open-instance" dismissible>
      {message}
    </Banner>,
  );
  await userEvent.click(screen.getByRole("button", { name: "Dismiss" }));
  expect(screen.queryByText(message)).not.toBeInTheDocument();
  unmount();

  render(
    <Banner tone="warn" id="open-instance" dismissible>
      {message}
    </Banner>,
  );
  expect(screen.queryByText(message)).not.toBeInTheDocument();
});

test("a banner with no id is not dismissible storage-wise and just shows", () => {
  render(<Banner>Reporting to GitHub is not connected.</Banner>);
  expect(screen.getByText("Reporting to GitHub is not connected.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Dismiss" })).not.toBeInTheDocument();
});
