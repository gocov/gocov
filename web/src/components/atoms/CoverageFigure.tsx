import { level, pct } from "@/lib/format";
import "./CoverageFigure.css";

/** The headline percentage of a page or a summary tile. */
export function CoverageFigure({ value, size = "lg" }: { value: number | null; size?: "lg" | "md" }) {
  const cls = `CoverageFigure CoverageFigure--${size}`;
  if (value === null) return <span className={`${cls} CoverageFigure--none`}>&mdash;</span>;
  return <span className={`${cls} CoverageFigure--${level(value)}`}>{pct(value)}</span>;
}
