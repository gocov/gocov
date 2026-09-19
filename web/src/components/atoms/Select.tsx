import type { ComponentPropsWithRef } from "react";
import { Icon } from "./Icon";
import "./Select.css";

/** A native select, styled down to the same box as the inputs. */
export function Select({ className, children, ...rest }: ComponentPropsWithRef<"select">) {
  return (
    <span className="Select">
      <select className={["Select__el", className].filter(Boolean).join(" ")} {...rest}>
        {children}
      </select>
      <span className="Select__caret">
        <Icon name="caret-down" />
      </span>
    </span>
  );
}
