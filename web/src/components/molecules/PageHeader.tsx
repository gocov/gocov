import type { ReactNode } from "react";
import "./PageHeader.css";

/**
 * The top of every page: the trail, the title, one muted line about it, and
 * the page's own actions on the right. The actions wrap under on a phone.
 */
export function PageHeader({
  breadcrumbs,
  title,
  meta,
  actions,
}: {
  breadcrumbs?: ReactNode;
  title: ReactNode;
  meta?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <header className="PageHeader">
      <div className="PageHeader__main">
        {breadcrumbs}
        <h1 className="PageHeader__title">{title}</h1>
        {meta !== undefined && <p className="PageHeader__meta">{meta}</p>}
      </div>
      {actions !== undefined && <div className="PageHeader__actions">{actions}</div>}
    </header>
  );
}
