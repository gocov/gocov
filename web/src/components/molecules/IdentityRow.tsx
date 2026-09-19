import type { ReactNode } from "react";
import "./IdentityRow.css";

/** Who acts on your behalf: the account posting statuses, the bot, the uploader. */
export function IdentityRow({
  avatar,
  id,
  description,
  chip,
}: {
  avatar?: ReactNode;
  id: ReactNode;
  description?: ReactNode;
  chip?: ReactNode;
}) {
  return (
    <div className="IdentityRow">
      {avatar}
      <div className="IdentityRow__text">
        <span className="IdentityRow__id">{id}</span>
        {description !== undefined && <span className="IdentityRow__desc">{description}</span>}
      </div>
      {chip !== undefined && <div className="IdentityRow__chip">{chip}</div>}
    </div>
  );
}
