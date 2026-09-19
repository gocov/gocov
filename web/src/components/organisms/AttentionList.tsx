import { LinkButton, Mono } from "@/components/atoms";
import { Card } from "@/components/molecules";
import type { AttentionRow } from "@/lib/dashboard";
import "./AttentionList.css";

/**
 * What the workspace should be looked at for, most severe first. The server
 * sends the facts; lib/dashboard.ts turns them into rows (attentionRows) and
 * this lists them. Nothing to report renders nothing at all.
 */
export function AttentionList({ rows }: { rows: AttentionRow[] }) {
  if (rows.length === 0) return null;
  return (
    <Card>
      <Card.Body flush>
        <ul className="AttentionList">
          {rows.map((copy) => {
            return (
              <li className="AttentionList__item" key={copy.key}>
                <span className="AttentionList__mark">
                  <span className={`AttentionList__dot AttentionList__dot--${copy.tone}`} role="img" aria-label={copy.status} />
                </span>
                <span className="AttentionList__text">
                  <span className="AttentionList__title">
                    {copy.before}
                    <Mono>{copy.name}</Mono>
                    {copy.after}
                  </span>
                  <span className="AttentionList__message">{copy.message}</span>
                </span>
                <LinkButton size="sm" to={copy.to}>
                  {copy.action}
                </LinkButton>
              </li>
            );
          })}
        </ul>
      </Card.Body>
    </Card>
  );
}
