import { useId, type ReactNode } from "react";
import "./FormField.css";

export interface FieldProps {
  id: string;
  "aria-describedby": string | undefined;
  "aria-invalid": true | undefined;
}

/**
 * A labelled control with its help and its error already wired: the field is
 * a function of the props it must carry, so nothing can drift apart.
 */
export function FormField({
  label,
  help,
  error,
  children,
}: {
  label: ReactNode;
  help?: ReactNode;
  error?: ReactNode;
  children: (field: FieldProps) => ReactNode;
}) {
  const id = useId();
  const helpId = `${id}-help`;
  const errorId = `${id}-error`;
  const described = [help !== undefined ? helpId : "", error !== undefined ? errorId : ""].filter(Boolean).join(" ");

  return (
    <div className="FormField">
      <label className="FormField__label" htmlFor={id}>
        {label}
      </label>
      {children({
        id,
        "aria-describedby": described === "" ? undefined : described,
        "aria-invalid": error !== undefined ? true : undefined,
      })}
      {help !== undefined && (
        <span className="FormField__help" id={helpId}>
          {help}
        </span>
      )}
      {error !== undefined && (
        <span className="FormField__error" id={errorId}>
          {error}
        </span>
      )}
    </div>
  );
}
