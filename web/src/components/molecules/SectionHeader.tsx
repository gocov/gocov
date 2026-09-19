import type { ReactNode } from "react";
import "./SectionHeader.css";

/** A section title with room for a note or a control on the right. */
export function SectionHeader({ title, children, id }: { title: ReactNode; children?: ReactNode; id?: string }) {
  return (
    <div className="SectionHeader">
      <h2 className="SectionHeader__title" id={id}>
        {title}
      </h2>
      {children !== undefined && <div className="SectionHeader__aside">{children}</div>}
    </div>
  );
}
