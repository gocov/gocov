import type { ReactNode } from "react";
import { CoverageFigure, Delta, Icon } from "@/components/atoms";
import { Card } from "@/components/molecules";
import type { Verdict } from "@/lib/api/types";
import "./VerdictCard.css";

const states = {
  pass: { label: "Gate passing", tone: "good" },
  fail: { label: "Gate failing", tone: "bad" },
  neutral: { label: "Coverage recorded", tone: "neutral" },
} as const;

/**
 * Where a report stands: the gate's answer, the headline figure and the prose
 * behind it, with the page's own facts about the commit beside it. The state
 * is said in words as well as colour.
 */
export function VerdictCard({ verdict, meta }: { verdict: Verdict; meta?: ReactNode }) {
  const state = states[verdict.state] ?? states.neutral;
  return (
    <Card>
      <Card.Body>
        <div className="VerdictCard">
          <div className="VerdictCard__main">
            <p className={`VerdictCard__state VerdictCard__state--${state.tone}`}>
              {verdict.state === "neutral" ? (
                <span className="VerdictCard__dot" />
              ) : (
                <Icon name={verdict.state === "pass" ? "check" : "cross"} size={14} />
              )}
              {state.label}
            </p>
            <p className="VerdictCard__figure">
              <CoverageFigure value={verdict.coverage} />
              <Delta value={verdict.delta} />
            </p>
            <p className="VerdictCard__reason">{verdict.reason}</p>
          </div>
          {meta !== undefined && <div className="VerdictCard__meta">{meta}</div>}
        </div>
      </Card.Body>
    </Card>
  );
}
