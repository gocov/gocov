import type { ReactNode } from "react";
import "./CodeBlock.css";

/**
 * A copyable block — a CI snippet, a command. It scrolls sideways rather than
 * wrapping, and takes focus so a keyboard can scroll it.
 */
export function CodeBlock({ children, label }: { children: ReactNode; label?: string }) {
  return (
    <pre className="CodeBlock" tabIndex={0} role="group" aria-label={label ?? "Code"}>
      <code>{children}</code>
    </pre>
  );
}
