import { Chip, CopyField } from "gocov-web";

export function WithPreview() {
  return (
    <CopyField
      label="Badge markdown"
      value="![coverage](https://app.gocov.dev/badge/github/acme/api)"
      preview={<Chip tone="good">coverage 74.0%</Chip>}
    />
  );
}

export function PlainValue() {
  return <CopyField label="Badge URL" value="https://app.gocov.dev/badge/github/acme/api" />;
}

export function Snippet() {
  return <CopyField label="Upload command" value="gocov upload --token $GOCOV_TOKEN coverage.out" />;
}
