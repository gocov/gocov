import type { ComponentPropsWithRef } from "react";
import "./Toggle.css";

type Props = Omit<ComponentPropsWithRef<"button">, "onChange" | "type" | "children"> & {
  checked: boolean;
  onChange: (checked: boolean) => void;
  /** The accessible name — the switch carries no visible text. */
  label: string;
};

/** An on/off switch: a real button with role="switch", not a styled checkbox. */
export function Toggle({ checked, onChange, label, className, disabled, ...rest }: Props) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      className={["Toggle", checked && "Toggle--on", className].filter(Boolean).join(" ")}
      onClick={() => onChange(!checked)}
      {...rest}
    >
      <span className="Toggle__knob" />
    </button>
  );
}
