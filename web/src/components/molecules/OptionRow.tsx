import type { ReactNode } from "react";
import "./OptionRow.css";

/** A thing you may act on: a workspace to register, a repo to connect. */
export function OptionRow({
  avatar,
  name,
  status,
  action,
}: {
  avatar?: ReactNode;
  name: ReactNode;
  status?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="OptionRow">
      {avatar}
      <div className="OptionRow__text">
        <span className="OptionRow__name">{name}</span>
        {status !== undefined && <span className="OptionRow__status">{status}</span>}
      </div>
      {action !== undefined && <div className="OptionRow__action">{action}</div>}
    </div>
  );
}
