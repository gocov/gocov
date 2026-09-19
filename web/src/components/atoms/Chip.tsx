import type { ReactNode } from "react";
import "./Chip.css";

export type Tone = "good" | "warn" | "bad" | "accent" | "neutral" | "plain";

export function Chip({ tone = "neutral", children }: { tone?: Tone; children: ReactNode }) {
  return <span className={`Chip${tone === "neutral" ? "" : ` Chip--${tone}`}`}>{children}</span>;
}
