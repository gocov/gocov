import type { ReactNode } from "react";
import "./Card.css";

/** The card's title bar: a heading, and room for a chip or a control. */
function CardHeader({ title, actions, id }: { title: ReactNode; actions?: ReactNode; id?: string }) {
  return (
    <div className="Card__header">
      <h2 className="Card__title" id={id}>
        {title}
      </h2>
      {actions !== undefined && <div className="Card__actions">{actions}</div>}
    </div>
  );
}

/** `flush` drops the padding so a table can reach the card's edges. */
function CardBody({ children, flush }: { children: ReactNode; flush?: boolean }) {
  return <div className={`Card__body${flush ? " Card__body--flush" : ""}`}>{children}</div>;
}

/** The place for the card's own actions and the line of advice beside them. */
function CardFooter({ children }: { children: ReactNode }) {
  return <div className="Card__footer">{children}</div>;
}

function CardRoot({ children, danger, className }: { children: ReactNode; danger?: boolean; className?: string }) {
  return <section className={["Card", danger && "Card--danger", className].filter(Boolean).join(" ")}>{children}</section>;
}

/** The bordered surface everything on a page sits in. */
export const Card = Object.assign(CardRoot, { Header: CardHeader, Body: CardBody, Footer: CardFooter });
