import { BeforeAfter, Mono } from "gocov-web";

export function Improved() {
  return <BeforeAfter before={71.2} after={74} />;
}

export function Regressed() {
  return <BeforeAfter before={88.4} after={81} />;
}

export function NewFile() {
  return <BeforeAfter before={null} after={62.5} />;
}

export function InAFileList() {
  return (
    <div className="stack stack-1">
      <div className="row row-2">
        <Mono>internal/server/upload.go</Mono>
        <BeforeAfter before={71.2} after={74} />
      </div>
      <div className="row row-2">
        <Mono>internal/core/pipeline.go</Mono>
        <BeforeAfter before={88.4} after={81} />
      </div>
      <div className="row row-2">
        <Mono>internal/diffcov/diff.go</Mono>
        <BeforeAfter before={null} after={62.5} />
      </div>
    </div>
  );
}
