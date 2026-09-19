import "./Spinner.css";

/**
 * Work in progress, small enough to sit inside a button or a notice. `label`
 * null is the decorative form, for a place whose own words already say what
 * is happening (a button reading "Saving…").
 */
export function Spinner({ label = "Working" }: { label?: string | null }) {
  if (label === null) return <span className="Spinner" aria-hidden="true" />;
  return <span className="Spinner" role="status" aria-label={label} />;
}
