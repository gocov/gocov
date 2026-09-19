import type { ReactNode } from "react";
import type { Gate } from "@/lib/api/types";
import { gateActive, gateLabel } from "@/lib/settings";
import { Chip } from "@/components/atoms";
import { Card, GateRow } from "@/components/molecules";

interface Props {
  gate: Gate;
  /** Workspace gates are inherited by its repos; a repo's are its own. */
  scope: "workspace" | "repo";
  readOnly?: boolean;
  onChange: (gate: Gate) => void;
  /** The card's own Save button and the line beside it. */
  footer?: ReactNode;
}

const appliesTo = {
  // A workspace gate is a default, not a fallback: it is copied into a
  // repository when the repository registers (core.registerRepo) and never
  // read again, so existing repositories keep whatever they have.
  workspace: "They are the starting gates of every repository registered in this workspace from now on; existing repositories keep their own.",
  repo: "They apply to this repository only.",
};

const minCoverageHelp = {
  workspace: "Fails when the whole project drops below this figure.",
  repo: "Fails when the whole repository drops below this figure.",
};

/** The three rules that decide whether a commit passes, and their figures. */
export function GatesCard({ gate, scope, readOnly, onChange, footer }: Props) {
  const set = (patch: Partial<Gate>) => onChange({ ...gate, ...patch });
  return (
    <Card>
      <Card.Header
        title="Coverage gates"
        actions={<Chip tone={gateActive(gate) === 0 ? "neutral" : "accent"}>{gateLabel(gate)}</Chip>}
      />
      <Card.Body>
        <div className="stack">
          <p>Gates decide whether a commit passes. {appliesTo[scope]} Leave a gate off to disable that rule.</p>
          <div className="stack stack-1">
            <GateRow
              name="Minimum total coverage"
              help={minCoverageHelp[scope]}
              value={gate.min_coverage}
              onChange={(v) => set({ min_coverage: v })}
              readOnly={readOnly}
            />
            <GateRow
              name="Minimum diff coverage"
              help="Fails when the lines changed in a pull/merge request are covered below this figure."
              value={gate.min_diff_coverage}
              whenOn={70}
              onChange={(v) => set({ min_diff_coverage: v })}
              readOnly={readOnly}
            />
            <GateRow
              name="Maximum coverage drop"
              help="Fails when total coverage falls by more than this against the base branch."
              value={gate.max_coverage_drop}
              whenOn={2}
              onChange={(v) => set({ max_coverage_drop: v })}
              readOnly={readOnly}
            />
          </div>
        </div>
      </Card.Body>
      {footer !== undefined && <Card.Footer>{footer}</Card.Footer>}
    </Card>
  );
}
