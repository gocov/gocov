import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { McpRefusalCard } from "./McpRefusalCard";

test("says why and where going back leads, and goes only when asked", async () => {
  const onBack = vi.fn();
  render(
    <McpRefusalCard
      client="Claude"
      redirectOrigin="https://claude.ai"
      refusal="PKCE with code_challenge_method=S256 is required"
      onBack={onBack}
    />,
  );
  expect(screen.getByRole("heading", { name: "This request cannot go ahead" })).toBeInTheDocument();
  expect(screen.getByText(/Claude asked in a way gocov does not accept: PKCE/)).toBeInTheDocument();
  expect(screen.getByText("https://claude.ai")).toBeInTheDocument();
  expect(onBack).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "Back to https://claude.ai" }));
  expect(onBack).toHaveBeenCalledOnce();
});

test("without a reason or a name it still reads", () => {
  render(<McpRefusalCard client="" redirectOrigin="myapp:" refusal="" onBack={() => {}} />);
  expect(screen.getByText("The client asked in a way gocov does not accept.")).toBeInTheDocument();
});
