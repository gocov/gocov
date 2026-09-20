import { Button, LinkButton } from "gocov-web";

export function Variants() {
  return (
    <div className="row">
      <Button>Rotate token</Button>
      <Button variant="primary">Save</Button>
      <Button variant="danger">Delete</Button>
      <Button variant="quiet">Sign out</Button>
    </div>
  );
}

export function Sizes() {
  return (
    <div className="row">
      <Button icon="copy">Copy snippet</Button>
      <Button size="sm" icon="copy">
        Copy
      </Button>
      <Button size="sm" variant="primary">
        Register
      </Button>
    </div>
  );
}

export function States() {
  return (
    <div className="row">
      <Button disabled>Unavailable</Button>
      <Button variant="primary" loading>
        Saving…
      </Button>
      <Button variant="danger" disabled>
        Owners only
      </Button>
    </div>
  );
}

export function AsLinks() {
  return (
    <div className="row">
      <LinkButton to="/">Dashboard</LinkButton>
      <LinkButton to="/" variant="primary">
        Add a repository
      </LinkButton>
      <LinkButton href="https://docs.gocov.dev" external icon="external">
        Docs
      </LinkButton>
    </div>
  );
}
