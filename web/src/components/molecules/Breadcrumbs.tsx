import type { ReactNode } from "react";
import { Link } from "react-router";
import { Icon } from "../atoms";
import "./Breadcrumbs.css";

export interface Crumb {
  label: ReactNode;
  /** Omitted on the last item — the page you are on. */
  to?: string;
}

/** The trail above a page title. The last item is the current page. */
export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <nav className="Breadcrumbs" aria-label="Breadcrumb">
      <ol className="Breadcrumbs__list">
        {items.map((item, i) => {
          const last = i === items.length - 1;
          return (
            <li key={i} className="Breadcrumbs__item">
              {i > 0 && (
                <span className="Breadcrumbs__sep">
                  <Icon name="caret-right" size={12} />
                </span>
              )}
              {item.to !== undefined && !last ? (
                <Link className="Breadcrumbs__label" to={item.to}>
                  {item.label}
                </Link>
              ) : (
                <span className="Breadcrumbs__label" aria-current={last ? "page" : undefined}>
                  {item.label}
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
