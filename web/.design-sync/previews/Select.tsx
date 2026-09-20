import { Select } from "gocov-web";

export function Retention() {
  return (
    <Select aria-label="Keep reports for" defaultValue="365">
      <option value="90">90 days</option>
      <option value="365">1 year</option>
      <option value="0">Forever</option>
    </Select>
  );
}

export function WithALabel() {
  return (
    <label className="stack stack-1">
      <span className="small muted">Compare against</span>
      <Select aria-label="Compare against" defaultValue="default">
        <option value="default">The default branch (main)</option>
        <option value="base">The pull request base</option>
        <option value="previous">The previous upload on this branch</option>
      </Select>
    </label>
  );
}

export function Disabled() {
  return (
    <Select aria-label="Keep reports for" defaultValue="90" disabled>
      <option value="90">90 days</option>
    </Select>
  );
}
