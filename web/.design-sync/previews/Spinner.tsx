import { Button, Notice, Spinner } from "gocov-web";

export function OnItsOwn() {
  return <Spinner />;
}

export function InASentence() {
  return (
    <span className="row">
      <Spinner label="Rotating" /> Rotating the upload token…
    </span>
  );
}

export function InANotice() {
  return <Notice busy>Fetching the diff for acme/api#412 from GitHub…</Notice>;
}

export function InAButton() {
  return (
    <div className="row">
      <Button variant="primary" loading>
        Saving…
      </Button>
      <span className="muted small">
        <Spinner label="Loading uploads" /> Loading uploads for main
      </span>
    </div>
  );
}
