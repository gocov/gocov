import type { AnchorHTMLAttributes, ButtonHTMLAttributes, ReactNode } from "react";
import { Link } from "react-router";
import { Icon, type IconName } from "./Icon";
import { Spinner } from "./Spinner";
import "./Button.css";

type Variant = "default" | "primary" | "danger" | "quiet";
type Size = "md" | "sm";

interface Look {
  variant?: Variant;
  size?: Size;
  icon?: IconName;
  children: ReactNode;
}

const cls = ({ variant = "default", size = "md" }: Pick<Look, "variant" | "size">, extra?: string) =>
  ["Button", variant !== "default" && `Button--${variant}`, size === "sm" && "Button--sm", extra].filter(Boolean).join(" ");

interface ButtonProps extends Look, Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children"> {
  /**
   * The action this button started is still running: a spinner takes the
   * icon's place and the button cannot be pressed again. The words are the
   * caller's — "Saving…" says more than a spinner does.
   */
  loading?: boolean;
}

export function Button({ variant, size, icon, loading = false, disabled, children, className, type = "button", ...rest }: ButtonProps) {
  return (
    <button
      type={type}
      className={cls({ variant, size }, className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading ? <Spinner label={null} /> : icon && <Icon name={icon} />}
      {children}
    </button>
  );
}

type LinkButtonProps = Look &
  Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href" | "children"> & {
    to?: string;
    href?: string;
    external?: boolean;
  };

/**
 * A link that looks like a button: `to` stays in the app, `href` leaves it.
 * Everything else an anchor takes — onClick, aria-*, data-* — reaches the
 * element either way.
 */
export function LinkButton({ to, href, external, variant, size, icon, children, className, ...rest }: LinkButtonProps) {
  const content = (
    <>
      {icon && <Icon name={icon} />}
      {children}
    </>
  );
  if (to !== undefined) {
    return (
      <Link to={to} className={cls({ variant, size }, className)} {...rest}>
        {content}
      </Link>
    );
  }
  return (
    <a href={href} className={cls({ variant, size }, className)} {...(external ? { target: "_blank", rel: "noopener" } : {})} {...rest}>
      {content}
    </a>
  );
}
