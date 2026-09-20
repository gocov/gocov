import { Chip, KeyValue, KeyValueList, Mono } from "gocov-web";

export function RowsInAList() {
  return (
    <KeyValueList>
      <KeyValue label="Received" value="3 hours ago" />
      <KeyValue label="Profile" value={<Mono>coverage.out · 184 kB</Mono>} />
      <KeyValue label="Format" value="go" />
    </KeyValueList>
  );
}

export function RichValues() {
  return (
    <KeyValueList>
      <KeyValue label="Commit" value={<Mono>a1b2c3d</Mono>} />
      <KeyValue label="Branch" value={<Mono>main</Mono>} />
      <KeyValue label="Gate" value={<Chip tone="good">Passing</Chip>} />
    </KeyValueList>
  );
}

export function InlineRows() {
  return (
    <KeyValueList layout="inline">
      <KeyValue label="Uploaded by" value={<Mono>gocov-action v1.17.0</Mono>} />
      <KeyValue label="CI run" value="GitHub Actions #2184" />
    </KeyValueList>
  );
}
