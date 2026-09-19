import type { ComponentPropsWithRef } from "react";
import "./Textarea.css";

type Props = ComponentPropsWithRef<"textarea"> & { mono?: boolean; invalid?: boolean };

/** Multi-line text. `mono` for the ones that hold paths or patterns. */
export function Textarea({ mono, invalid, className, rows = 4, ...rest }: Props) {
  return (
    <textarea
      rows={rows}
      className={["Textarea", mono && "Textarea--mono", invalid && "Textarea--invalid", className].filter(Boolean).join(" ")}
      aria-invalid={invalid || undefined}
      {...rest}
    />
  );
}
