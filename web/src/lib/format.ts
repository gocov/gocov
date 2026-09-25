// Presentation helpers. The API sends numbers and timestamps; everything a
// person reads is produced here, so it is said one way everywhere.

export type Level = "good" | "warn" | "bad";

/** The coverage thresholds: below 50 bad, up to 75 warn, above good. */
export const level = (pct: number): Level => (pct < 50 ? "bad" : pct <= 75 ? "warn" : "good");

export const pct = (v: number) => v.toFixed(1) + "%";

/** Go's %.4g, the way a gate threshold reads: 60 is "60", 82.55 stays "82.55". */
export const sig4 = (v: number) => String(Number(v.toPrecision(4)));

/** SVG coordinates stay at one decimal, so the markup stays compact. */
export const round1 = (v: number) => Math.round(v * 10) / 10;

export const shortSha = (sha: string) => (sha.length > 12 ? sha.slice(0, 12) : sha);

export const humanInt = (n: number) => n.toLocaleString("en-US");

export const plural = (n: number, one: string, many = one + "s") => `${n} ${n === 1 ? one : many}`;

// The coverage thresholds, the trend's dead band, the forge names and the
// app's posting identity are the server's too (badge colours, the files
// card, OIDC refusals, the dashboard): internal/server/testdata/
// presentation.json pins both sides (presentation.test.ts here).

const forgeLabels: Record<string, string> = { github: "GitHub", gitlab: "GitLab", bitbucket: "Bitbucket" };

/** Who the GitHub App's statuses and comments appear as. */
export const appAccount = "gocov[bot]";

/** A forge's proper name — "GitHub", "GitLab", "Bitbucket" — or the name capitalized for one it does not know. */
export const forgeLabel = (forge: string) => forgeLabels[forge] ?? forge.charAt(0).toUpperCase() + forge.slice(1);

const ciLabels: Record<string, string> = {
  github: "GitHub Actions",
  gitlab: "GitLab CI",
  bitbucket: "Bitbucket Pipelines",
};

/** The CI service an upload came from, by the provider code the API sends; "" for one it does not know. */
export const ciLabel = (provider: string) => ciLabels[provider] ?? "";

const uploaderKinds: Record<string, string> = { cli: "CLI", action: "Action" };

/** "CLI" or "Action", by the uploader kind the API sends; "" for one it does not know. */
export const uploaderKindLabel = (kind: string) => uploaderKinds[kind] ?? "";

/** A byte count as a compact size: "999 B", "2 KB" (whole KB), "3.5 MB". */
export function humanBytes(n: number): string {
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n >= 1 << 10) return Math.round(n / (1 << 10)) + " KB";
  return n + " B";
}

/** A processing time: "412 ms" under a second, "1.5 s" from one on. */
export const duration = (ms: number) => (ms >= 1000 ? (ms / 1000).toFixed(1) + " s" : `${ms} ms`);

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
