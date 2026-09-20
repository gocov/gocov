import { Logo } from "gocov-web";

export function TopBar() {
  return (
    <span className="row">
      <Logo />
      <strong>gocov</strong>
      <span className="muted small">acme/api</span>
    </span>
  );
}

export function SignInCard() {
  return (
    <span className="stack stack-1">
      <Logo size={34} />
      <span className="muted small">Sign in to gocov</span>
    </span>
  );
}

export function Sizes() {
  return (
    <span className="row row-2">
      <Logo size={18} />
      <Logo size={34} />
      <Logo size={64} />
    </span>
  );
}
