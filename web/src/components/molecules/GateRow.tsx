import { useId, useState, type ReactNode } from "react";
import { TextInput, Toggle } from "../atoms";
import "./GateRow.css";

interface Props {
  /** The rule, as a person says it: "Minimum total coverage". */
  name: string;
  help?: ReactNode;
  /** null = the rule is off. */
  value: number | null;
  onChange: (value: number | null) => void;
  /** The figure the rule starts at when it is switched on. */
  whenOn?: number;
  /** A member rather than an owner: the figure, no switch. */
  readOnly?: boolean;
}

/** One coverage-gate rule: what it does, the figure, and whether it applies. */
export function GateRow({ name, help, value, onChange, whenOn = 80, readOnly }: Props) {
  const id = useId();
  const helpId = `${id}-help`;
  // While the box is being edited it holds text, which may be halfway to a
  // number ("" or "7."); the committed value stays whatever last parsed.
  const [draft, setDraft] = useState<string | null>(null);
  const on = value !== null;
  const text = draft ?? (value === null ? "" : String(value));

  function edit(next: string) {
    setDraft(next);
    const n = Number.parseFloat(next);
    if (!Number.isNaN(n)) onChange(Math.min(100, Math.max(0, n)));
  }

  // An empty box means the rule is off — the same contract the server uses.
  function commit() {
    setDraft(null);
    if (text.trim() === "") onChange(null);
  }

  return (
    <div className={`GateRow${on ? "" : " GateRow--off"}`}>
      <div className="GateRow__text">
        <label className="GateRow__name" htmlFor={id}>
          {name}
        </label>
        {help !== undefined && (
          <span className="GateRow__help" id={helpId}>
            {help}
          </span>
        )}
      </div>
      <div className="GateRow__control">
        <TextInput
          className="GateRow__input"
          id={id}
          type="number"
          min={0}
          max={100}
          step={0.1}
          value={text}
          disabled={!on || readOnly}
          aria-describedby={help === undefined ? undefined : helpId}
          onChange={(e) => edit(e.target.value)}
          onBlur={commit}
        />
        <span className="GateRow__unit">%</span>
        {!readOnly && (
          <Toggle
            checked={on}
            label={name}
            onChange={(next) => {
              setDraft(null);
              onChange(next ? whenOn : null);
            }}
          />
        )}
      </div>
    </div>
  );
}
