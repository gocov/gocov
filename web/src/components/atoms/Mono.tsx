import type { ComponentPropsWithRef } from "react";
import "./Mono.css";

/** Slugs, SHAs, branches, paths and env names — anything you would retype. */
export function Mono({ className, children, ...rest }: ComponentPropsWithRef<"span">) {
  return (
    <span className={["Mono", className].filter(Boolean).join(" ")} {...rest}>
      {children}
    </span>
  );
}
