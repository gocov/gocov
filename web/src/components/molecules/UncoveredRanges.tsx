import "./UncoveredRanges.css";

/**
 * The line ranges a report never reached, as the API sends them ("12-18, 40").
 * A long list is cut short: the point is the shape of the gap, not every line.
 */
export function UncoveredRanges({ ranges, max = 8 }: { ranges: string | string[]; max?: number }) {
  const all = (typeof ranges === "string" ? ranges.split(",") : ranges).map((r) => r.trim()).filter(Boolean);
  if (all.length === 0) return <span className="UncoveredRanges UncoveredRanges--none">&mdash;</span>;

  const shown = all.slice(0, max);
  const rest = all.length - shown.length;
  return (
    <span className="UncoveredRanges">
      {shown.map((range, i) => (
        <span key={i} className="UncoveredRanges__range">
          {range}
        </span>
      ))}
      {rest > 0 && <span className="UncoveredRanges__more">+{rest} more</span>}
    </span>
  );
}
