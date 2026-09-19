import "./Logo.css";

/**
 * The gocov mark: a coverage ring, most of the way round. Decorative — the
 * word "gocov" always sits beside it. Drawn from the tokens, so it follows
 * the theme; the favicon (internal/server/static/favicon.svg) is its fixed
 * twin.
 */
export function Logo({ size = 18 }: { size?: number }) {
  return (
    <svg className="Logo" viewBox="0 0 32 32" width={size} height={size} aria-hidden="true">
      <circle className="Logo__track" cx="16" cy="16" r="12.5" />
      <circle className="Logo__arc" cx="16" cy="16" r="12.5" strokeDasharray="61.3 78.5" transform="rotate(-90 16 16)" />
    </svg>
  );
}
