import type { ReactNode } from "react";
import { CopyButton } from "./CopyButton";
import "./CopyField.css";

/**
 * A value you are meant to take away — a badge URL, a snippet. Read-only, so
 * selecting it is safe, with an optional preview (the badge image) in front.
 */
export function CopyField({
  value,
  label = "Value",
  preview,
}: {
  value: string;
  /** The accessible name of the field. */
  label?: string;
  preview?: ReactNode;
}) {
  return (
    <div className="CopyField">
      {preview !== undefined && <span className="CopyField__preview">{preview}</span>}
      <input className="CopyField__input" aria-label={label} value={value} readOnly />
      <CopyButton value={value} />
    </div>
  );
}
