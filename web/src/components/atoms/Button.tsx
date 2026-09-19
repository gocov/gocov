import type { ButtonHTMLAttributes, ReactNode } from "react";
import { Link } from "react-router";
import { Icon, type IconName } from "./Icon";
import "./Button.css";

type Variant = "default" | "primary" | "danger" | "quiet";
type Size = "md" | "sm";

interface Look {
  variant?: Variant;
  size?: Size;
  icon?: IconName;
  children: ReactNode;
}

const cls = ({ variant = "default", size = "md" }: Look, extra?: string) =>
  ["Button", variant !== "default" && `Button--${variant}`, size === "sm" && "Button--sm", extra].filter(Boolean).join(" ");

export function Button({ variant, size, icon, children, className, type = "button", ...rest }: Look & ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button type={type} className={cls({ variant, size, children }, className)} {...rest}>
      {icon && <Icon name={icon} />}
      {children}
    </button>
  );
}

/** A link that looks like a button: `to` stays in the app, `href` leaves it. */
export function LinkButton({ to, href, external, ...look }: Look & { to?: string; href?: string; external?: boolean }) {
  const content = (
    <>
      {look.icon && <Icon name={look.icon} />}
      {look.children}
    </>
  );
  if (to !== undefined) {
    return (
      <Link to={to} className={cls(look)}>
        {content}
      </Link>
    );
  }
  return (
    <a href={href} className={cls(look)} {...(external ? { target: "_blank", rel: "noopener" } : {})}>
      {content}
    </a>
  );
}
