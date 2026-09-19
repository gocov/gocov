import { useState, type ReactNode } from "react";
import { Button, Chip, Spinner } from "../atoms";
import { CopyButton } from "./CopyButton";
import "./SecretField.css";

interface Props {
  /** The environment variable the value belongs in, e.g. GOCOV_TOKEN. */
  name: string;
  kind: "Secret" | "Variable";
  note?: ReactNode;
  /** A variable's plain value; for a secret, leave it out and pass `masked`. */
  value?: string;
  masked?: string;
  /** Fetched once, the first time the value is revealed or copied. */
  onReveal?: () => Promise<string>;
  /** A viewer who may not see the value at all: the header, and nothing else. */
  locked?: boolean;
}

/**
 * One credential as CI needs it: the variable name, what kind of value it is,
 * and the value itself — masked until asked for. The value carries
 * data-ph-no-capture so session replay never records a token.
 */
export function SecretField({ name, kind, note, value, masked, onReveal, locked }: Props) {
  const [revealed, setRevealed] = useState<string | undefined>(value);
  const [shown, setShown] = useState(value !== undefined);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function fetchValue(): Promise<string> {
    if (revealed !== undefined) return revealed;
    if (!onReveal) return "";
    setBusy(true);
    setError("");
    try {
      const text = await onReveal();
      setRevealed(text);
      return text;
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not fetch the value.");
      throw e;
    } finally {
      setBusy(false);
    }
  }

  async function toggle() {
    if (shown) {
      setShown(false);
      return;
    }
    try {
      await fetchValue();
      setShown(true);
    } catch {
      // The error is on screen; leave the value masked.
    }
  }

  return (
    <div className="SecretField">
      <div className="SecretField__head">
        <span className="SecretField__name">{name}</span>
        <Chip tone="plain">{kind}</Chip>
        {note !== undefined && <span className="SecretField__note">{note}</span>}
      </div>
      {!locked && (
        <div className="SecretField__body">
          <span className="SecretField__value" data-ph-no-capture>
            {shown ? (revealed ?? "") : (masked ?? "")}
          </span>
          {busy && <Spinner label={`Fetching ${name}`} />}
          {onReveal !== undefined && (
            <Button size="sm" icon={shown ? "eye-off" : "eye"} onClick={() => void toggle()} disabled={busy}>
              {shown ? "Hide" : "Reveal"}
            </Button>
          )}
          <CopyButton size="sm" value={() => fetchValue()} />
        </div>
      )}
      {error !== "" && (
        <p className="SecretField__error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
