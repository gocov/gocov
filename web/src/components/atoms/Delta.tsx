import { deltaText, trend } from "@/lib/format";
import { Icon, type IconName } from "./Icon";
import "./Delta.css";

const arrows: Record<"up" | "down", IconName> = {
  up: "arrow-up",
  down: "arrow-down",
};

/**
 * A coverage change. The sign is in the text as well as the colour, so the
 * direction survives a monochrome screen. Nothing to report is a quiet dash
 * either way — no baseline (`null`), or a change that rounds to 0.0 — told
 * apart only in the accessible name: a column of "0.0%" is noise, and the
 * rows that did move are what the eye should land on.
 */
export function Delta({ value }: { value: number | null }) {
  if (value === null) return <span className="Delta Delta--none" role="img" aria-label="No baseline">&mdash;</span>;
  const t = trend(value);
  if (t === "flat") return <span className="Delta Delta--flat" role="img" aria-label="No change">&mdash;</span>;
  return (
    <span className={`Delta Delta--${t}`}>
      <Icon name={arrows[t]} size={12} />
      {deltaText(value)}
    </span>
  );
}
