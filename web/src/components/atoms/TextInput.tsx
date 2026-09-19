import type { ComponentPropsWithRef } from "react";
import { Icon } from "./Icon";
import "./TextInput.css";

type Props = Omit<ComponentPropsWithRef<"input">, "type"> & {
  type?: "text" | "search" | "number";
  invalid?: boolean;
};

/** One line of text. `type="search"` carries the magnifier. */
export function TextInput({ type = "text", invalid, className, ...rest }: Props) {
  const input = (
    <input
      type={type}
      className={["TextInput", invalid && "TextInput--invalid", type === "search" && "TextInput--search", className]
        .filter(Boolean)
        .join(" ")}
      aria-invalid={invalid || undefined}
      {...rest}
    />
  );
  if (type !== "search") return input;
  return (
    <span className="TextInput__wrap">
      <span className="TextInput__icon">
        <Icon name="search" />
      </span>
      {input}
    </span>
  );
}
