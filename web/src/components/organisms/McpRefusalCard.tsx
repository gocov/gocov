import { Button, Logo, Mono, Notice } from "@/components/atoms";
import { Card } from "@/components/molecules";
import "./McpRefusalCard.css";

/**
 * An MCP authorization request gocov refuses: who asked, why it cannot go
 * ahead, and the way back to the client — offered, never taken by itself,
 * because a client's name and address are only its registrant's word.
 */
export function McpRefusalCard({
  client,
  redirectOrigin,
  refusal,
  onBack,
}: {
  client: string;
  redirectOrigin: string;
  refusal: string;
  onBack: () => void;
}) {
  return (
    <div className="McpRefusalCard">
      <Card>
        <Card.Body>
          <div className="stack">
            <div className="McpRefusalCard__head">
              <Logo size={34} />
              <h1>This request cannot go ahead</h1>
            </div>
            <Notice tone="bad">
              {client || "The client"} asked in a way gocov does not accept{refusal ? `: ${refusal}` : "."}
            </Notice>
            <p className="muted">
              Going back sends you to <Mono>{redirectOrigin}</Mono>. Only go there if you started this from it;
              otherwise close this page.
            </p>
            <div className="McpRefusalCard__actions">
              <Button onClick={onBack}>Back to {redirectOrigin}</Button>
            </div>
          </div>
        </Card.Body>
      </Card>
    </div>
  );
}
