import type { ReactNode } from "react";
import "./SegmentedControl.css";

export interface SegmentOption {
  value: string;
  label: ReactNode;
  /** A count beside the label, e.g. how many rows the filter keeps. */
  count?: number;
  disabled?: boolean;
}

/** A small set of exclusive choices — a filter or a view mode, never a form field. */
export function SegmentedControl({
  options,
  value,
  onChange,
  label,
}: {
  options: SegmentOption[];
  value: string;
  onChange: (value: string) => void;
  label: string;
}) {
  return (
    <div className="SegmentedControl" role="group" aria-label={label}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          className="SegmentedControl__option"
          aria-pressed={option.value === value}
          disabled={option.disabled}
          onClick={() => onChange(option.value)}
        >
          {option.label}
          {option.count !== undefined && <span className="SegmentedControl__count">{option.count}</span>}
        </button>
      ))}
    </div>
  );
}
