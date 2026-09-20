import { Textarea } from "gocov-web";

export function Plain() {
  return (
    <Textarea
      aria-label="Why this gate exists"
      defaultValue="We ignore the generated protobuf code here — the gate on acme/api would never be reachable otherwise."
    />
  );
}

export function MonoPaths() {
  return (
    <Textarea aria-label="Ignore paths" mono defaultValue={"vendor/**\n**/*_test.go\ninternal/mock/**"} />
  );
}

export function Invalid() {
  return <Textarea aria-label="Ignore paths" mono invalid defaultValue={"vendor/**\n[unclosed"} />;
}

export function Disabled() {
  return (
    <Textarea
      aria-label="Ignore paths"
      mono
      disabled
      defaultValue={"vendor/**\n**/*_test.go"}
    />
  );
}
