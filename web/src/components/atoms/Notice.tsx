import type { ReactNode } from "react";
import { Icon, type IconName } from "./Icon";
import { Spinner } from "./Spinner";
import "./Notice.css";

export type NoticeTone = "neutral" | "good" | "warn" | "bad";

const icons: Partial<Record<NoticeTone, IconName>> = { good: "check", warn: "warning", bad: "cross" };

/**
 * A short explanation attached to what it is about. The tone is a mark in the
 * margin, never a tinted background — the text stays as readable as the page.
 */
export function Notice({ tone = "neutral", busy, children }: { tone?: NoticeTone; busy?: boolean; children: ReactNode }) {
  const icon = icons[tone];
  return (
    <div className={`Notice Notice--${tone}`} role={tone === "bad" ? "alert" : busy ? "status" : undefined}>
      <span className="Notice__mark">
        {busy ? <Spinner /> : icon ? <Icon name={icon} /> : <span className="Notice__dot" />}
      </span>
      <div className="Notice__body">{children}</div>
    </div>
  );
}
