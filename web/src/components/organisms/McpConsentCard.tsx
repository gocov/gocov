import { Button, Logo, Mono, Notice } from "@/components/atoms";
import { Card } from "@/components/molecules";
import "./McpConsentCard.css";

/**
 * The MCP connector's consent: which client asks to read reports as whom, and
 * where an approval is sent — the one thing that tells a real client from a
 * made-up one.
 */
export function McpConsentCard({
  client,
  redirectOrigin,
  user,
  busy,
  error,
  onApprove,
  onDeny,
}: {
  client: string;
  redirectOrigin: string;
  user: string;
  busy: boolean;
  error: string | null;
  onApprove: () => void;
  onDeny: () => void;
}) {
  return (
    <div className="McpConsentCard">
      <Card>
        <Card.Body>
          <div className="stack">
            <div className="McpConsentCard__head">
              <Logo size={34} />
              <h1>Connect {client} to gocov</h1>
            </div>
            {error && <Notice tone="bad">{error}</Notice>}
            <p>
              {client} asks to read coverage reports as <strong>{user}</strong>: the repos of your workspaces, and
              public ones. It cannot change anything.
            </p>
            <p className="muted">
              Approving sends you to <Mono>{redirectOrigin}</Mono>. Only approve if you started this from there.
            </p>
            <div className="McpConsentCard__actions">
              <Button onClick={onDeny} disabled={busy}>
                Cancel
              </Button>
              <Button variant="primary" onClick={onApprove} loading={busy}>
                Approve
              </Button>
            </div>
          </div>
        </Card.Body>
      </Card>
    </div>
  );
}
