import { CopyButton } from "gocov-web";

export function Default() {
  return <CopyButton value="gocov upload coverage.out" />;
}

export function SmallWithLabel() {
  return <CopyButton size="sm" label="Copy markdown" value="![coverage](https://app.gocov.dev/badge/github/acme/api)" />;
}

export function Primary() {
  return <CopyButton variant="primary" label="Copy GOCOV_TOKEN" value="gocov_live_9f2c41d8a7b3" />;
}

export function Together() {
  return (
    <div className="row">
      <CopyButton value="gocov upload coverage.out" />
      <CopyButton size="sm" label="Copy commit" value="a1b2c3d" />
      <CopyButton size="sm" variant="primary" label="Copy workflow" value="uses: gocov-dev/gocov-action@v1" />
    </div>
  );
}
