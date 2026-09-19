import type { ComponentPropsWithRef, ReactNode } from "react";
import { Icon } from "./Icon";
import "./Checkbox.css";

type Props = Omit<ComponentPropsWithRef<"input">, "type" | "children"> & { label: ReactNode };

/** A checkbox and its label are one target: the whole row toggles. */
export function Checkbox({ label, className, ...rest }: Props) {
  return (
    <label className={["Checkbox", className].filter(Boolean).join(" ")}>
      <input type="checkbox" className="Checkbox__input" {...rest} />
      <span className="Checkbox__box" aria-hidden="true">
        <Icon name="check" size={12} />
      </span>
      <span className="Checkbox__label">{label}</span>
    </label>
  );
}
