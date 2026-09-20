import { KeyValue, KeyValueList, Mono } from "gocov-web";

export function Stacked() {
  return (
    <KeyValueList>
      <KeyValue label="Received" value="3 hours ago" />
      <KeyValue label="Profile" value={<Mono>coverage.out · 184 kB</Mono>} />
      <KeyValue label="Format" value="go" />
    </KeyValueList>
  );
}

export function Inline() {
  return (
    <KeyValueList layout="inline">
      <KeyValue label="Uploaded by" value={<Mono>gocov-action v1.17.0</Mono>} />
      <KeyValue label="CI run" value="GitHub Actions #2184" />
      <KeyValue label="Parts" value="3 of 3 merged" />
    </KeyValueList>
  );
}

export function ASinglePair() {
  return (
    <KeyValueList>
      <KeyValue label="Default branch" value={<Mono>main</Mono>} />
    </KeyValueList>
  );
}
