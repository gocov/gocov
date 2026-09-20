import { Icon } from "gocov-web";

const names = [
  "check",
  "cross",
  "caret-down",
  "caret-right",
  "arrow-up",
  "arrow-down",
  "arrow-left",
  "arrow-right",
  "minus",
  "search",
  "external",
  "copy",
  "eye",
  "eye-off",
  "folder",
  "file",
  "warning",
  "info",
  "person",
  "bot",
] as const;

export function TheWholeSet() {
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "repeat(auto-fill, minmax(130px, 1fr))",
        gap: "var(--space-1)",
      }}
    >
      {names.map((name) => (
        <span key={name} className="row">
          <Icon name={name} size={16} />
          <span className="mono muted">{name}</span>
        </span>
      ))}
    </div>
  );
}

export function Sizes() {
  return (
    <div className="row row-2">
      <span className="row">
        <Icon name="folder" size={12} /> <span className="mono muted">12</span>
      </span>
      <span className="row">
        <Icon name="folder" size={16} /> <span className="mono muted">16 · the default</span>
      </span>
      <span className="row">
        <Icon name="folder" size={20} /> <span className="mono muted">20</span>
      </span>
      <span className="row">
        <Icon name="folder" size={28} /> <span className="mono muted">28</span>
      </span>
    </div>
  );
}

export function TakesTheColourAround() {
  return (
    <div className="stack stack-1">
      <span className="row" style={{ color: "var(--good)" }}>
        <Icon name="check" size={16} /> Gate passing — diff coverage 82.0%
      </span>
      <span className="row" style={{ color: "var(--warn)" }}>
        <Icon name="warning" size={16} /> No upload in 21 days
      </span>
      <span className="row" style={{ color: "var(--bad)" }}>
        <Icon name="cross" size={16} /> Gate failing — total coverage 41.4%
      </span>
      <span className="row muted">
        <Icon name="info" size={16} /> Coverage is statement-weighted.
      </span>
    </div>
  );
}

export function InAFileTree() {
  return (
    <div className="stack stack-1 mono">
      <span className="row">
        <Icon name="caret-down" size={16} />
        <Icon name="folder" size={16} /> internal/server
      </span>
      <span className="row">
        <Icon name="file" size={16} /> upload.go
      </span>
      <span className="row">
        <Icon name="file" size={16} /> spa.go
      </span>
      <span className="row">
        <Icon name="caret-right" size={16} />
        <Icon name="folder" size={16} /> internal/core
      </span>
    </div>
  );
}
