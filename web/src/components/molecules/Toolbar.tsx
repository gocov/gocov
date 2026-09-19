import type { ReactNode } from "react";
import "./Toolbar.css";

/** A row of controls above a table or a list: filters left, status right. */
export function Toolbar({ left, right, label }: { left?: ReactNode; right?: ReactNode; label?: string }) {
  return (
    <div className="Toolbar" role={label ? "group" : undefined} aria-label={label}>
      <div className="Toolbar__side">{left}</div>
      <div className="Toolbar__side Toolbar__side--right">{right}</div>
    </div>
  );
}
