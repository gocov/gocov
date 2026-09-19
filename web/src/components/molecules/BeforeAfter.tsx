import { pct } from "@/lib/format";
import { Icon } from "../atoms";
import "./BeforeAfter.css";

/** What a file's coverage was and what it is now. No baseline: a dash. */
export function BeforeAfter({ before, after }: { before: number | null; after: number }) {
  return (
    <span className="BeforeAfter">
      <span className="BeforeAfter__before">{before === null ? <>&mdash;</> : pct(before)}</span>
      <Icon name="arrow-right" size={12} />
      <span className="BeforeAfter__after">{pct(after)}</span>
    </span>
  );
}
