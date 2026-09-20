import { Toggle } from "gocov-web";

export function On() {
  return (
    <span className="row">
      <Toggle checked onChange={() => {}} label="Post statuses back" />
      <span>Post statuses back to GitHub</span>
    </span>
  );
}

export function Off() {
  return (
    <span className="row">
      <Toggle checked={false} onChange={() => {}} label="Comment on pull requests" />
      <span>Comment on pull requests</span>
    </span>
  );
}

export function Disabled() {
  return (
    <span className="stack stack-1">
      <span className="row">
        <Toggle checked onChange={() => {}} label="Public reports" disabled />
        <span className="muted">Public reports</span>
      </span>
      <span className="muted small">Owners only.</span>
    </span>
  );
}

export function ASettingsList() {
  return (
    <div className="stack stack-1">
      <span className="row">
        <Toggle checked onChange={() => {}} label="Fail the build below the gate" />
        <span>Fail the build below the gate</span>
      </span>
      <span className="row">
        <Toggle checked={false} onChange={() => {}} label="Carry coverage forward" />
        <span>Carry coverage forward when a file is untouched</span>
      </span>
      <span className="row">
        <Toggle checked onChange={() => {}} label="Email me when a gate breaks" />
        <span>Email me when a gate breaks on main</span>
      </span>
    </div>
  );
}
