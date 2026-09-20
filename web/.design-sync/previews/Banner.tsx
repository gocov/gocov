import { Banner, Button } from "gocov-web";

export function Neutral() {
  return (
    <Banner action={<Button size="sm">Set it up</Button>}>
      Sign-in is not configured: this instance is open to anyone who can reach it.
    </Banner>
  );
}

export function Warn() {
  return (
    <Banner tone="warn">
      Reporting to Bitbucket stopped working &mdash; statuses and comments are being skipped.
    </Banner>
  );
}

export function WarnDismissible() {
  return (
    <Banner tone="warn" id="preview-grant-expired" dismissible action={<Button size="sm">Reconnect</Button>}>
      The GitLab grant for acme expired &mdash; gocov cannot fetch diffs until it is renewed.
    </Banner>
  );
}
