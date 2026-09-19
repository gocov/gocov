import { useState, type ReactNode } from "react";
import { Button, Notice } from "@/components/atoms";
import { Card, ConfirmDialog } from "@/components/molecules";

interface Props {
  title: string;
  /** What the move takes with it, said plainly. */
  children: ReactNode;
  actionLabel: string;
  /** The sentence the confirmation asks. */
  confirmText: ReactNode;
  /** The line beside the button — "This cannot be undone." */
  hint: ReactNode;
  owner: boolean;
  /** What a member is told instead of being offered the button. */
  ownerOnlyHint?: ReactNode;
  onConfirm: () => Promise<void>;
}

/** The last card of a settings page: one move, behind one confirmation. */
export function DangerCard({
  title,
  children,
  actionLabel,
  confirmText,
  hint,
  owner,
  ownerOnlyHint = "Only a workspace owner can do this.",
  onConfirm,
}: Props) {
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function run() {
    setConfirming(false);
    setBusy(true);
    setError("");
    try {
      await onConfirm();
    } catch (e) {
      setError(e instanceof Error ? e.message : "That did not work.");
      setBusy(false);
    }
  }

  return (
    <Card danger>
      <Card.Header title={title} />
      <Card.Body>
        <div className="stack">
          {error !== "" && <Notice tone="bad">{error}</Notice>}
          <div>{children}</div>
        </div>
      </Card.Body>
      <Card.Footer>
        {owner ? (
          <>
            <Button variant="danger" loading={busy} onClick={() => setConfirming(true)}>
              {actionLabel}
            </Button>
            <span>{hint}</span>
          </>
        ) : (
          <span>{ownerOnlyHint}</span>
        )}
      </Card.Footer>
      {owner && (
        <ConfirmDialog
          open={confirming}
          danger
          title={title}
          confirmLabel={actionLabel}
          onConfirm={() => void run()}
          onCancel={() => setConfirming(false)}
        >
          {confirmText}
        </ConfirmDialog>
      )}
    </Card>
  );
}
