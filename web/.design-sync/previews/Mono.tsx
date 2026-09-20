import { Mono } from "gocov-web";

export function Identifiers() {
  return <Mono>a1b2c3d4e5f6 · acme/api · main</Mono>;
}

export function InASentence() {
  return (
    <span>
      The gate on <Mono>acme/api</Mono> compares <Mono>fix/upload</Mono> against <Mono>main</Mono>, and the last upload
      on that branch was <Mono>a1b2c3d</Mono>.
    </span>
  );
}

export function Paths() {
  return (
    <span className="stack stack-1">
      <Mono>internal/server/upload.go</Mono>
      <Mono>internal/core/pipeline.go</Mono>
      <Mono>web/src/lib/api/types.ts</Mono>
    </span>
  );
}

export function NextToMutedText() {
  return (
    <span className="row">
      <Mono>GOCOV_TOKEN</Mono>
      <span className="muted small">set once, in your CI secrets</span>
    </span>
  );
}
