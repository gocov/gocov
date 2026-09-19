import type { Trend } from "@/lib/format";
import "./Sparkline.css";

// Geometry taken from newSparkView in internal/server/dashboard.go, so the
// glyph keeps the shape the dashboard this replaced drew.
const TOP = 3;
const BOT = 19;
const WIDTH = 76;
const MAX_POINTS = 12;

const round1 = (v: number) => Math.round(v * 10) / 10;

/**
 * A repo's recent coverage as a 76×22 glyph, oldest point first. Fewer than
 * two points is a dash. `stale` compresses the line into the left 60% and
 * continues it with a dashed flat tail: the series simply stopped.
 */
export function Sparkline({ series, stale = false, label }: { series: number[]; stale?: boolean; label?: string }) {
  const points = series.slice(-MAX_POINTS);
  if (points.length < 2) return <span className="Sparkline Sparkline--none">&mdash;</span>;

  const lo = Math.min(...points);
  const hi = Math.max(...points);
  const right = stale ? 46 : WIDTH;
  const y = (v: number) => (hi === lo ? (TOP + BOT) / 2 : round1(BOT - ((v - lo) / (hi - lo)) * (BOT - TOP)));
  const path = points
    .map((v, i) => `${i === 0 ? "M" : " L"}${round1((right * i) / (points.length - 1))} ${y(v)}`)
    .join("");

  const first = points[0] ?? 0;
  const last = points[points.length - 1] ?? 0;
  // A stopped series has no meaningful direction.
  const dir: Trend = stale || Math.abs(last - first) < 0.05 ? "flat" : last > first ? "up" : "down";
  const tailY = y(last);

  return (
    <svg
      className={`Sparkline Sparkline--${dir}`}
      viewBox={`0 0 ${WIDTH} 22`}
      width={WIDTH}
      height={22}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      role="img"
      aria-label={label ?? `Coverage trend: ${stale ? "stopped" : dir}`}
    >
      <path d={path} />
      {stale && <path className="Sparkline__tail" d={`M${right} ${tailY} L${WIDTH} ${tailY}`} />}
    </svg>
  );
}
