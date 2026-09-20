import { TextInput } from "gocov-web";

export function DefaultBranch() {
  return (
    <label className="stack stack-1">
      <span className="small muted">Default branch</span>
      <TextInput aria-label="Default branch" defaultValue="main" />
    </label>
  );
}

export function Types() {
  return (
    <div className="stack stack-1">
      <TextInput type="search" aria-label="Search files" placeholder="Search files…" />
      <TextInput type="number" aria-label="Minimum coverage" defaultValue={80} />
    </div>
  );
}

export function States() {
  return (
    <div className="stack stack-1">
      <TextInput aria-label="Branch" defaultValue="mian" invalid />
      <TextInput aria-label="Workspace slug" defaultValue="acme" disabled />
    </div>
  );
}

export function Empty() {
  return <TextInput aria-label="Repository" placeholder="acme/api" />;
}
