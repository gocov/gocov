import type { ReactNode } from "react";
import "./EmptyState.css";

/** Nothing here yet, and what to do about it. */
export function EmptyState({ message, action }: { message: ReactNode; action?: ReactNode }) {
  return (
    <div className="EmptyState">
      <p className="EmptyState__message">{message}</p>
      {action !== undefined && <div className="EmptyState__action">{action}</div>}
    </div>
  );
}
