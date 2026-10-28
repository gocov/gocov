import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { McpConsent } from "@/lib/api/types";
import { leaveFor } from "@/lib/urls";
import { mockApi, renderPage } from "@/test/render";
import McpAuthorizePage from "./McpAuthorizePage";

vi.mock("@/lib/urls", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/urls")>()),
  leaveFor: vi.fn(),
}));

const consent: McpConsent = {
  client: "Claude",
  redirect_origin: "https://claude.ai",
  deny_url: "https://claude.ai/cb?error=access_denied&state=s",
  user: "Jane Dev",
};
const path = "/mcp/authorize?client_id=mcp_x&state=s";

const show = (handlers: Record<string, unknown>) => {
  const fetch = mockApi(handlers);
  renderPage(<McpAuthorizePage />, { route: "mcp/authorize", path });
  return fetch;
};

beforeEach(() => vi.mocked(leaveFor).mockClear());

test("asks the server about its own query, and approving leaves with the code", async () => {
  const fetch = show({
    "GET /mcp-authorize": consent,
    "POST /mcp-authorize/approve": { redirect: "https://claude.ai/cb?code=c&state=s" },
  });
  await userEvent.click(await screen.findByRole("button", { name: "Approve" }));
  await waitFor(() => expect(leaveFor).toHaveBeenCalledWith("https://claude.ai/cb?code=c&state=s"));
  const [read, post] = fetch.mock.calls;
  expect(String(read?.[0])).toBe("/api/ui/mcp-authorize?client_id=mcp_x&state=s");
  expect(JSON.parse(String(post?.[1]?.body))).toEqual({ query: "?client_id=mcp_x&state=s" });
});

test("cancelling goes back to the client with the denial", async () => {
  show({ "GET /mcp-authorize": consent });
  await userEvent.click(await screen.findByRole("button", { name: "Cancel" }));
  expect(leaveFor).toHaveBeenCalledWith(consent.deny_url);
});

test("a request to refuse goes straight back", async () => {
  show({ "GET /mcp-authorize": { redirect: "https://claude.ai/cb?error=invalid_request" } });
  await waitFor(() => expect(leaveFor).toHaveBeenCalledWith("https://claude.ai/cb?error=invalid_request"));
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
});

test("a link gocov did not issue says so", async () => {
  show({ "GET /mcp-authorize": { status: 422, error: "This sign-in link is not one gocov issued, or it was changed on the way." } });
  expect(await screen.findByRole("alert")).toHaveTextContent("This sign-in link is not one gocov issued");
});
