import "./Spinner.css";

/** Work in progress, small enough to sit inside a button or a notice. */
export function Spinner({ label = "Working" }: { label?: string }) {
  return <span className="Spinner" role="status" aria-label={label} />;
}
