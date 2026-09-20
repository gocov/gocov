import { Notice } from "gocov-web";

export function Neutral() {
  return <Notice>Coverage still uploads; only posting back to the forge has stopped.</Notice>;
}

export function Good() {
  return <Notice tone="good">Connected. Statuses and comments post as gocov[bot].</Notice>;
}

export function Warn() {
  return <Notice tone="warn">No uploads in 21 days — this repo&rsquo;s coverage is stale.</Notice>;
}

export function Bad() {
  return <Notice tone="bad">The grant was revoked on GitLab. Nothing has been posted back since.</Notice>;
}

export function Busy() {
  return <Notice busy>Rotating the upload token…</Notice>;
}

export function Stacked() {
  return (
    <div className="stack stack-1">
      <Notice tone="good">acme/api uploaded a1b2c3d — 74.0%, up 2.8 points.</Notice>
      <Notice tone="warn">acme/web has no gate configured, so nothing can fail.</Notice>
    </div>
  );
}
