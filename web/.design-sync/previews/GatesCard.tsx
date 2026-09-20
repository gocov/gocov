import { useState } from "react";
import { GatesCard, SaveFooter } from "gocov-web";

type Gate = { min_coverage: number | null; min_diff_coverage: number | null; max_coverage_drop: number | null };

export function WorkspaceGates() {
  const [gate, setGate] = useState<Gate>({ min_coverage: 80, min_diff_coverage: 70, max_coverage_drop: null });
  return (
    <GatesCard
      gate={gate}
      scope="workspace"
      onChange={setGate}
      footer={
        <SaveFooter
          owner
          hint="Applies to the next upload."
          ownerOnly="Owners set the gates."
          busy={false}
          saving={false}
          saved={false}
          onSave={() => {}}
        />
      }
    />
  );
}

export function RepositoryGates() {
  const [gate, setGate] = useState<Gate>({ min_coverage: 74, min_diff_coverage: null, max_coverage_drop: 2 });
  return (
    <GatesCard
      gate={gate}
      scope="repo"
      onChange={setGate}
      footer={
        <SaveFooter
          owner
          hint="Applies to uploads received from now on. Past verdicts are not recalculated."
          ownerOnly="Owners set the gates."
          busy={false}
          saving={false}
          saved
          onSave={() => {}}
        />
      }
    />
  );
}

export function NoGateSetYet() {
  const [gate, setGate] = useState<Gate>({ min_coverage: null, min_diff_coverage: null, max_coverage_drop: null });
  return <GatesCard gate={gate} scope="repo" onChange={setGate} />;
}

export function ReadOnlyForAMember() {
  return (
    <GatesCard
      gate={{ min_coverage: 80, min_diff_coverage: null, max_coverage_drop: 2 }}
      scope="repo"
      readOnly
      onChange={() => {}}
      footer={
        <SaveFooter
          owner={false}
          hint="Applies to the next upload."
          ownerOnly="Owners set the gates."
          busy={false}
          saving={false}
          saved={false}
          onSave={() => {}}
        />
      }
    />
  );
}
