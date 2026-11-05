import { useMutation, useQuery } from "@tanstack/react-query";
import { useLocation } from "react-router";
import { QueryBoundary } from "@/components/molecules";
import { McpConsentCard, McpRefusalCard } from "@/components/organisms";
import { apiPost, errorMessage } from "@/lib/api/client";
import { mcpConsentQuery } from "@/lib/api/queries";
import type { McpApproveInput, McpConsent } from "@/lib/api/types";
import { usePageTitle } from "@/lib/title";
import { leaveFor } from "@/lib/urls";

/**
 * /mcp/authorize: an MCP client (a claude.ai connector, say) asks to read
 * reports as the signed-in user. The request is this page's own query
 * string; the server reads it, and the approval sends the browser back to
 * the client with a code.
 */
export default function McpAuthorizePage() {
  const { search } = useLocation();
  const query = useQuery(mcpConsentQuery(search));
  usePageTitle("connect an MCP client");

  const approve = useMutation({
    mutationFn: () => apiPost<McpConsent>("/mcp-authorize/approve", { query: search } satisfies McpApproveInput),
    onSuccess: (result) => {
      if (result.redirect) leaveFor(result.redirect);
    },
  });

  // A request to refuse goes back to its client with the error, but only
  // when the user chooses to: the client and its address are whatever its
  // registrant said, so a link must never bounce a visitor there unseen.
  return (
    <QueryBoundary query={query}>
      {(consent) =>
        consent.redirect ? (
          <McpRefusalCard
            client={consent.client ?? ""}
            redirectOrigin={consent.redirect_origin ?? ""}
            refusal={consent.refusal ?? ""}
            onBack={() => consent.redirect && leaveFor(consent.redirect)}
          />
        ) : (
          <McpConsentCard
            client={consent.client ?? ""}
            redirectOrigin={consent.redirect_origin ?? ""}
            user={consent.user ?? ""}
            busy={approve.isPending || approve.isSuccess}
            error={approve.isError ? errorMessage(approve.error, "The approval did not go through. Please try again.") : null}
            onApprove={() => approve.mutate()}
            onDeny={() => consent.deny_url && leaveFor(consent.deny_url)}
          />
        )
      }
    </QueryBoundary>
  );
}
