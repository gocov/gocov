import { Children, type CSSProperties, type ReactNode } from "react";
import "./StatRow.css";

/** One figure with its label. Lives inside a StatRow. */
export function StatTile({ label, value, hint }: { label: ReactNode; value: ReactNode; hint?: ReactNode }) {
  return (
    <div className="StatTile">
      <span className="StatTile__label">{label}</span>
      <span className="StatTile__value">{value}</span>
      {hint !== undefined && <span className="StatTile__hint">{hint}</span>}
    </div>
  );
}

/**
 * Two to four tiles as one bordered card, divided by hairlines rather than
 * split into separate boxes. Two-up on a narrow screen.
 */
export function StatRow({ children }: { children: ReactNode }) {
  const cols = Math.max(1, Children.count(children));
  return (
    <div className="StatRow" style={{ "--stat-cols": cols } as CSSProperties}>
      {children}
    </div>
  );
}
