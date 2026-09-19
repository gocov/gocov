import { useState, type ReactNode } from "react";
import { Button, Mono, Notice } from "@/components/atoms";
import { Card, ConfirmDialog, CopyButton, SecretField } from "@/components/molecules";
import "./TokenCard.css";

interface Props {
  title: string;
  /** What this token is for, in the card's own words. */
  intro: ReactNode;
  /** GOCOV_SERVER for a self-hosted instance; null on the hosted service. */
  serverUrl: string | null;
  /** The masked token; null for a viewer who may not see it at all. */
  tokenMasked: string | null;
  owner: boolean;
  onReveal: () => Promise<string>;
  onRotate: () => Promise<string>;
}

/**
 * The credentials CI needs, and the one move that changes them. The token
 * itself never leaves this component: it is fetched on demand and held in
 * state, never in the query cache, storage or the URL.
 */
export function TokenCard({ title, intro, serverUrl, tokenMasked, owner, onReveal, onRotate }: Props) {
  const [confirming, setConfirming] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [newToken, setNewToken] = useState("");
  const [error, setError] = useState("");
  // Rotating invalidates whatever the field has already revealed, so the
  // field starts again from the new masked value.
  const [rotations, setRotations] = useState(0);

  async function rotate() {
    setConfirming(false);
    setRotating(true);
    setError("");
    try {
      const token = await onRotate();
      setNewToken(token);
      setRotations((n) => n + 1);
    } catch (e) {
      setError(e instanceof Error ? e.message : "The token could not be rotated.");
    } finally {
      setRotating(false);
    }
  }

  return (
    <Card>
      <Card.Header title={title} />
      <Card.Body>
        <div className="stack">
          <p>{intro}</p>
          {error !== "" && <Notice tone="bad">{error}</Notice>}
          {newToken !== "" && (
            <Notice tone="good">
              <div className="stack stack-1">
                <p>
                  <strong>Save it now — it is shown only this once.</strong> The previous token stopped working the
                  moment it was rotated.
                </p>
                <div className="row">
                  <Mono className="TokenCard__new" data-ph-no-capture>
                    {newToken}
                  </Mono>
                  <CopyButton size="sm" value={newToken} />
                </div>
              </div>
            </Notice>
          )}
          {serverUrl !== null && (
            <SecretField name="GOCOV_SERVER" kind="Variable" note="Your instance — plain value, not a secret" value={serverUrl} />
          )}
          <SecretField
            key={rotations}
            name="GOCOV_TOKEN"
            kind="Secret"
            note="Shown to workspace owners only"
            masked={tokenMasked ?? ""}
            locked={!owner}
            onReveal={owner ? onReveal : undefined}
          />
        </div>
      </Card.Body>
      <Card.Footer>
        {owner ? (
          <>
            <Button onClick={() => setConfirming(true)} disabled={rotating}>
              {rotating ? "Rotating…" : "Rotate token"}
            </Button>
            <span>The old token stops working the moment you rotate.</span>
          </>
        ) : (
          <span>Only a workspace owner can see or rotate the token.</span>
        )}
      </Card.Footer>
      {owner && (
        <ConfirmDialog
          open={confirming}
          danger
          title="Rotate the upload token?"
          confirmLabel="Rotate token"
          onConfirm={() => void rotate()}
          onCancel={() => setConfirming(false)}
        >
          The current token stops working immediately and CI must be updated.
        </ConfirmDialog>
      )}
    </Card>
  );
}
