import type { ReactNode } from "react";
import "./InlineCode.css";

/** A literal inside a sentence: a flag, a filename, an env var. */
export function InlineCode({ children }: { children: ReactNode }) {
  return <code className="InlineCode">{children}</code>;
}
