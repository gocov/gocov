// Presentation helpers. The API sends numbers and timestamps; everything a
// person reads is produced here, so it is said one way everywhere.

export type Level = "good" | "warn" | "bad";

/** The coverage thresholds: below 50 bad, up to 75 warn, above good. */
export const level = (pct: number): Level => (pct < 50 ? "bad" : pct <= 75 ? "warn" : "good");

export const pct = (v: number) => v.toFixed(1) + "%";

export const shortSha = (sha: string) => (sha.length > 12 ? sha.slice(0, 12) : sha);

export const humanInt = (n: number) => n.toLocaleString("en-US");

export const plural = (n: number, one: string, many = one + "s") => `${n} ${n === 1 ? one : many}`;

export type Trend = "up" | "down" | "flat";

/** A change under 0.05 points rounds to 0.0 and reads as flat. */
export const trend = (delta: number): Trend => (delta >= 0.05 ? "up" : delta <= -0.05 ? "down" : "flat");

export function deltaText(delta: number): string {
  const t = trend(delta);
  if (t === "flat") return "0.0%";
  return (t === "up" ? "+" : "−") + Math.abs(delta).toFixed(1) + "%";
}

export function timeAgo(iso: string, now: Date = new Date()): string {
  const then = new Date(iso);
  const mins = Math.floor((now.getTime() - then.getTime()) / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} min ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return hours === 1 ? "1 hour ago" : `${hours} hours ago`;
  const days = Math.floor(hours / 24);
  if (days < 14) return days === 1 ? "yesterday" : `${days} days ago`;
  return then.toISOString().slice(0, 10);
}

/** "total ≥ 80%, diff ≥ 70%" — empty when no rule is set. */
export function gateSummary(g: { min_coverage: number | null; min_diff_coverage: number | null; max_coverage_drop: number | null }): string {
  const parts: string[] = [];
  if (g.min_coverage !== null) parts.push(`total ≥ ${g.min_coverage}%`);
  if (g.min_diff_coverage !== null) parts.push(`diff ≥ ${g.min_diff_coverage}%`);
  if (g.max_coverage_drop !== null) parts.push(`drop ≤ ${g.max_coverage_drop}%`);
  return parts.join(", ");
}

/** "internal/server/" + "api.go" for a dimmed directory prefix. */
export function splitPath(path: string): [dir: string, base: string] {
  const i = path.lastIndexOf("/");
  return i < 0 ? ["", path] : [path.slice(0, i + 1), path.slice(i + 1)];
}
