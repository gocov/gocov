import { useId, type ReactNode } from "react";
import "./Tooltip.css";

/**
 * A short hint on hover or keyboard focus. It wraps non-interactive content —
 * the anchor takes the focus so a keyboard reaches the hint too.
 */
export function Tooltip({ text, children }: { text: string; children: ReactNode }) {
  const id = useId();
  return (
    <span className="Tooltip">
      <span className="Tooltip__anchor" tabIndex={0} aria-describedby={id}>
        {children}
      </span>
      <span className="Tooltip__bubble" role="tooltip" id={id}>
        {text}
      </span>
    </span>
  );
}
