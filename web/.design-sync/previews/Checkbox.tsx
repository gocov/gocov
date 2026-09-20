import { Checkbox } from "gocov-web";

export function CheckedAndUnchecked() {
  return (
    <div className="stack stack-1">
      <Checkbox label="Public reports" defaultChecked />
      <Checkbox label="Post a comment on every pull request" />
    </div>
  );
}

export function Disabled() {
  return (
    <div className="stack stack-1">
      <Checkbox label="Carry coverage forward (owners only)" disabled />
      <Checkbox label="Require diff coverage (owners only)" defaultChecked disabled />
    </div>
  );
}

export function ASettingsGroup() {
  return (
    <div className="stack stack-1">
      <Checkbox label="Fail the build when a gate fails" defaultChecked />
      <Checkbox label="Post statuses back to GitHub" defaultChecked />
      <Checkbox label="Skip files matched by vendor/**" />
      <Checkbox label="Email hello@gocov.dev when coverage drops" />
    </div>
  );
}

export function RichLabel() {
  return (
    <Checkbox
      label={
        <span>
          Make <span className="mono">acme/api</span> public —{" "}
          <span className="muted small">anyone with the link sees its coverage</span>
        </span>
      }
      defaultChecked
    />
  );
}
