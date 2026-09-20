import { Icon, Tooltip } from "gocov-web";

export function OnAWord() {
  return (
    <span className="row">
      Coverage is{" "}
      <Tooltip text="Statement-weighted across every repository in the workspace.">
        <span style={{ textDecoration: "underline dotted" }}>statement-weighted</span>
      </Tooltip>
    </span>
  );
}

export function OnAnIcon() {
  return (
    <span className="row">
      <span>Gate</span>
      <Tooltip text="This repository has no gate configured.">
        <Icon name="info" size={16} />
      </Tooltip>
    </span>
  );
}

export function InATableHeading() {
  return (
    <span className="row row-2">
      <span className="small muted">Repository</span>
      <span className="small muted">Coverage</span>
      <span className="row">
        <span className="small muted">Δ</span>
        <Tooltip text="Against the last upload on main: a1b2c3d, 71.2%.">
          <Icon name="info" size={16} />
        </Tooltip>
      </span>
      <span className="row">
        <span className="small muted">Diff</span>
        <Tooltip text="Only the lines this pull request touches.">
          <Icon name="info" size={16} />
        </Tooltip>
      </span>
    </span>
  );
}
