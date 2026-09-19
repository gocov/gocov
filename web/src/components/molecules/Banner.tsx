import { useState, type ReactNode } from "react";
import { Button, Icon } from "../atoms";
import "./Banner.css";

const key = (id: string) => `gocov.banner.${id}`;

/** Storage can be denied outright (private mode, blocked cookies). */
function dismissedBefore(id: string | undefined): boolean {
  if (id === undefined) return false;
  try {
    return sessionStorage.getItem(key(id)) === "1";
  } catch {
    return false;
  }
}

function rememberDismissal(id: string | undefined) {
  if (id === undefined) return;
  try {
    sessionStorage.setItem(key(id), "1");
  } catch {
    // A banner that cannot be remembered simply comes back.
  }
}

/**
 * A quiet strip above the page — an instance-wide notice, not a page's own
 * message. With an `id` it stays dismissed for the rest of the session.
 */
export function Banner({
  tone = "neutral",
  children,
  action,
  id,
  dismissible,
}: {
  tone?: "neutral" | "warn";
  children: ReactNode;
  action?: ReactNode;
  id?: string;
  dismissible?: boolean;
}) {
  const [gone, setGone] = useState(() => dismissedBefore(id));
  if (gone) return null;
  return (
    <div className={`Banner Banner--${tone}`}>
      {tone === "warn" && (
        <span className="Banner__mark">
          <Icon name="warning" />
        </span>
      )}
      <div className="Banner__body">{children}</div>
      {action !== undefined && <div className="Banner__action">{action}</div>}
      {dismissible && (
        <Button
          variant="quiet"
          size="sm"
          icon="cross"
          aria-label="Dismiss"
          onClick={() => {
            rememberDismissal(id);
            setGone(true);
          }}
        >
          {""}
        </Button>
      )}
    </div>
  );
}
