import { useMemo, useRef, useState, type ReactNode } from "react";
import { Button } from "@/components/atoms";
import { SegmentedControl, Toolbar } from "@/components/molecules";
import type { SourceLine } from "@/lib/api/types";
import { plural } from "@/lib/format";
import { buildSource, hitsLabel } from "@/lib/source";
import "./SourceViewer.css";

/**
 * A file's source with its coverage overlaid: a rail of uncovered blocks to
 * jump between, and the lines themselves. Colour means coverage and nothing
 * else — a 2px bar and a wash on the line, a dot for a line this commit lost.
 */
export function SourceViewer({ lines, newlyUncovered }: { lines: SourceLine[]; newlyUncovered: number }) {
  const model = useMemo(() => buildSource(lines), [lines]);
  const [only, setOnly] = useState("all");
  const [focus, setFocus] = useState(-1);
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  // The first line of each block, so a jump can scroll it into view.
  const starts = useRef<(HTMLDivElement | null)[]>([]);

  const { blocks, items, total, missLines } = model;
  const any = blocks.length > 0;

  function focusBlock(i: number) {
    if (!any) return;
    const at = ((i % blocks.length) + blocks.length) % blocks.length;
    setFocus(at);
    // jsdom has no scrollIntoView; the jump is then just the outline.
    starts.current[at]?.scrollIntoView?.({ block: "center" });
  }

  /** Next and previous cycle; from nowhere they enter at the near end. */
  const step = (d: number) => focusBlock(focus < 0 ? (d > 0 ? 0 : blocks.length - 1) : focus + d);

  const expand = (id: string) => setExpanded((prev) => new Set(prev).add(id));

  const rows: ReactNode[] = [];
  for (const item of items) {
    if (item.kind === "fold") {
      if (only === "miss" || expanded.has(item.fold.id)) continue;
      const { id, from, to, count } = item.fold;
      rows.push(
        <div key={`fold-${id}`} className="SourceViewer__fold">
          <span>{plural(count, "line")} hidden</span>
          <Button
            size="sm"
            variant="quiet"
            icon="caret-down"
            aria-label={`Expand ${count} hidden lines, ${from}–${to}`}
            onClick={() => expand(id)}
          >
            Expand
          </Button>
        </div>,
      );
      continue;
    }
    const { line, state, block } = item;
    if (item.fold !== null && !expanded.has(item.fold)) continue;
    if (only === "miss" && state !== "miss") continue;
    const start = block !== null && line.no === blocks[block]?.start;
    rows.push(
      <div
        key={line.no}
        className={`SourceViewer__line SourceViewer__line--${state}${block !== null && block === focus ? " SourceViewer__line--focus" : ""}`}
        ref={
          start
            ? (el) => {
                starts.current[block] = el;
              }
            : undefined
        }
      >
        <span className="SourceViewer__no">
          {line.new_miss && (
            <>
              <span className="SourceViewer__new" aria-hidden="true" />
              <span className="sr-only">newly uncovered</span>
            </>
          )}
          {line.no}
        </span>
        <span className="SourceViewer__hits">{hitsLabel(line.hits)}</span>
        <pre className="SourceViewer__text">{line.text}</pre>
      </div>,
    );
  }

  return (
    <div className="SourceViewer stack">
      <Toolbar
        label="Source tools"
        left={
          <>
            <SegmentedControl
              label="Line filter"
              value={only}
              onChange={setOnly}
              options={[
                { value: "all", label: "All lines" },
                { value: "miss", label: "Uncovered only", disabled: !any },
              ]}
            />
            <Button size="sm" icon="arrow-up" disabled={!any} onClick={() => step(-1)}>
              Previous miss
            </Button>
            <Button size="sm" icon="arrow-down" disabled={!any} onClick={() => step(1)}>
              Next miss
            </Button>
            {focus >= 0 && (
              <span className="SourceViewer__pos">
                Block {focus + 1} of {blocks.length}
              </span>
            )}
          </>
        }
        right={
          any
            ? `${plural(missLines, "uncovered line")} in ${plural(blocks.length, "block")}`
            : "Every statement in this file is covered."
        }
      />

      <div className="SourceViewer__panel">
        <div className="SourceViewer__rail" role="group" aria-label="Uncovered blocks">
          {blocks.map((b, i) => (
            <button
              key={b.start}
              type="button"
              className={`SourceViewer__marker${i === focus ? " SourceViewer__marker--focus" : ""}`}
              style={{ top: `${b.top.toFixed(2)}%`, height: `${b.height.toFixed(2)}%` }}
              onClick={() => focusBlock(i)}
            >
              <span className="sr-only">
                Lines {b.start}&ndash;{b.end} &middot; {b.lines} uncovered
              </span>
            </button>
          ))}
        </div>
        <div className="SourceViewer__code">{rows}</div>
      </div>

      <div className="SourceViewer__legend">
        <span className="SourceViewer__key">
          <span className="SourceViewer__swatch SourceViewer__swatch--hit" /> Executed
        </span>
        <span className="SourceViewer__key">
          <span className="SourceViewer__swatch SourceViewer__swatch--miss" /> Never executed
        </span>
        <span className="SourceViewer__key">
          <span className="SourceViewer__swatch SourceViewer__swatch--none" /> Not a statement
        </span>
        {newlyUncovered > 0 && (
          <span className="SourceViewer__key">
            <span className="SourceViewer__swatch SourceViewer__swatch--new" /> Newly uncovered by this commit
          </span>
        )}
        <span>The rail maps all {total} lines of the file.</span>
      </div>
    </div>
  );
}
