import { Button, Chip } from "../atoms";

interface Props {
  /** Members read the settings; only an owner gets the button. */
  owner: boolean;
  /** What saving this card does, beside the button. */
  hint: string;
  /** What a member reads in the button's place. */
  ownerOnly: string;
  busy: boolean;
  saving: boolean;
  saved: boolean;
  onSave: () => void;
}

/**
 * The inside of a settings card's footer: Save, what it applies to, and a
 * word once it worked. Every editable card carries the same button; only the
 * pressed one says it is working. Layout is Card.Footer's, so it has no CSS.
 */
export function SaveFooter({ owner, hint, ownerOnly, busy, saving, saved, onSave }: Props) {
  if (!owner) return <span>{ownerOnly}</span>;
  return (
    <>
      <Button variant="primary" onClick={onSave} disabled={busy} loading={saving}>
        {saving ? "Saving…" : "Save"}
      </Button>
      <span>{hint}</span>
      {saved && <Chip tone="good">Saved</Chip>}
    </>
  );
}
