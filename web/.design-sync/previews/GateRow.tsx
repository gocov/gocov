import { useState } from "react";
import { GateRow } from "gocov-web";

export function GatesForAnOwner() {
  const [total, setTotal] = useState<number | null>(80);
  const [diff, setDiff] = useState<number | null>(null);
  const [drop, setDrop] = useState<number | null>(2);
  return (
    <div className="stack stack-1">
      <GateRow
        name="Minimum total coverage"
        help="Fails when the whole project drops below this figure."
        value={total}
        onChange={setTotal}
      />
      <GateRow
        name="Minimum diff coverage"
        help="Fails when the lines changed in a pull request are covered below this figure."
        value={diff}
        onChange={setDiff}
        whenOn={70}
      />
      <GateRow
        name="Maximum coverage drop"
        help="Fails when total coverage falls by more than this against the base branch."
        value={drop}
        onChange={setDrop}
      />
    </div>
  );
}

export function SwitchedOn() {
  const [value, setValue] = useState<number | null>(74);
  return (
    <GateRow
      name="Minimum total coverage"
      help="Fails when the whole project drops below this figure."
      value={value}
      onChange={setValue}
    />
  );
}

export function SwitchedOff() {
  const [value, setValue] = useState<number | null>(null);
  return (
    <GateRow
      name="Minimum diff coverage"
      help="Switched off. The toggle starts it at 70%."
      value={value}
      onChange={setValue}
      whenOn={70}
    />
  );
}

export function ReadOnlyForAMember() {
  return (
    <GateRow
      name="Maximum coverage drop"
      help="Only workspace owners can change the gates."
      value={2}
      onChange={() => {}}
      readOnly
    />
  );
}
