import { Button, LinkButton } from "../atoms";
import "./Pagination.css";

export interface PageStep {
  /** An in-app link; otherwise give onClick. */
  to?: string;
  onClick?: () => void;
  disabled?: boolean;
}

function Step({ step, label, icon }: { step: PageStep | undefined; label: string; icon: "arrow-left" | "arrow-right" }) {
  const off = step === undefined || step.disabled === true || (step.to === undefined && step.onClick === undefined);
  if (off) {
    return (
      <Button icon={icon} disabled>
        {label}
      </Button>
    );
  }
  if (step.to !== undefined) {
    return (
      <LinkButton to={step.to} icon={icon}>
        {label}
      </LinkButton>
    );
  }
  return (
    <Button icon={icon} onClick={step.onClick}>
      {label}
    </Button>
  );
}

/** Time-ordered lists page one way: newer back up, older further down. */
export function Pagination({ newer, older }: { newer?: PageStep; older?: PageStep }) {
  return (
    <nav className="Pagination" aria-label="Pagination">
      <Step step={newer} label="Newer" icon="arrow-left" />
      <Step step={older} label="Older" icon="arrow-right" />
    </nav>
  );
}
