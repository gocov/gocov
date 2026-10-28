import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { McpConsentCard } from "./McpConsentCard";

const show = (props: Partial<Parameters<typeof McpConsentCard>[0]> = {}) => {
  const onApprove = vi.fn();
  const onDeny = vi.fn();
  render(
    <McpConsentCard
      client="Claude"
      redirectOrigin="https://claude.ai"
      user="Jane Dev"
      busy={false}
      error={null}
      onApprove={onApprove}
      onDeny={onDeny}
      {...props}
    />,
  );
  return { onApprove, onDeny };
};

test("names the client, the user and where an approval goes", async () => {
  const { onApprove, onDeny } = show();
  expect(screen.getByRole("heading", { name: "Connect Claude to gocov" })).toBeInTheDocument();
  expect(screen.getByText("Jane Dev")).toBeInTheDocument();
  expect(screen.getByText("https://claude.ai")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Approve" }));
  expect(onApprove).toHaveBeenCalledOnce();
  await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(onDeny).toHaveBeenCalledOnce();
});

test("an approval under way cannot be sent twice", () => {
  show({ busy: true });
  expect(screen.getByRole("button", { name: "Approve" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
});

test("a failed approval says why", () => {
  show({ error: "The approval did not go through." });
  expect(screen.getByRole("alert")).toHaveTextContent("The approval did not go through.");
});
