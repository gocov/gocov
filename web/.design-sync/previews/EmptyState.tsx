import { Button, EmptyState, LinkButton } from "gocov-web";

export function WithAction() {
  return <EmptyState message="No uploads on this branch yet." action={<Button variant="primary">Set up CI</Button>} />;
}

export function MessageOnly() {
  return <EmptyState message="No repository under acme has a gate configured." />;
}

export function WithLinkAction() {
  return (
    <EmptyState
      message="acme has no repositories yet — the first upload registers one."
      action={<LinkButton to="/">Read the setup guide</LinkButton>}
    />
  );
}
