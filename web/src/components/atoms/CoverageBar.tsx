import { level, pct } from "@/lib/format";
import "./CoverageBar.css";

/** The bar carries the threshold colour; the figure stays plain text. */
export function CoverageBar({ value }: { value: number }) {
  return (
    <span className="CoverageBar" role="img" aria-label={`${pct(value)} covered`}>
      <span className="CoverageBar__track">
        <span className={`CoverageBar__fill CoverageBar__fill--${level(value)}`} style={{ width: `${Math.max(0, Math.min(100, value))}%` }} />
      </span>
      <span className="CoverageBar__pct">{pct(value)}</span>
    </span>
  );
}
