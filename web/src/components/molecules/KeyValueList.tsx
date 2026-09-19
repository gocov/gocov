import type { ReactNode } from "react";
import "./KeyValueList.css";

/** One label/value pair. Always inside a KeyValueList. */
export function KeyValue({ label, value }: { label: ReactNode; value: ReactNode }) {
  return (
    <div className="KeyValue">
      <dt className="KeyValue__label">{label}</dt>
      <dd className="KeyValue__value">{value}</dd>
    </div>
  );
}

/** Provenance, settings summaries: stacked pairs, or inline on one line each. */
export function KeyValueList({ layout = "stacked", children }: { layout?: "stacked" | "inline"; children: ReactNode }) {
  return <dl className={`KeyValueList KeyValueList--${layout}`}>{children}</dl>;
}
