import { SecretField } from "gocov-web";

export function Secret() {
  return (
    <SecretField
      name="GOCOV_TOKEN"
      kind="Secret"
      note="Shown to workspace owners only"
      masked="gocov_live_••••••••••••"
      onReveal={() => Promise.resolve("gocov_live_9f2c41d8a7b3")}
    />
  );
}

export function Variable() {
  return (
    <SecretField
      name="GOCOV_SERVER"
      kind="Variable"
      note="Your instance — not a secret"
      value="https://gocov.example.com"
    />
  );
}

export function Locked() {
  return <SecretField name="GOCOV_TOKEN" kind="Secret" note="Shown to workspace owners only" locked />;
}
