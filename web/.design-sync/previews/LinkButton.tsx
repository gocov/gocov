import { LinkButton } from "gocov-web";

export function InAppLink() {
  return <LinkButton to="/">Back to acme/api</LinkButton>;
}

export function Variants() {
  return (
    <div className="row">
      <LinkButton to="/">Settings</LinkButton>
      <LinkButton to="/" variant="primary">
        Connect acme to GitHub
      </LinkButton>
      <LinkButton to="/" variant="danger">
        Remove this repository
      </LinkButton>
      <LinkButton to="/" variant="quiet">
        Skip for now
      </LinkButton>
    </div>
  );
}

export function External() {
  return (
    <div className="row">
      <LinkButton href="https://docs.gocov.dev/ci/github-actions" external icon="external">
        Wiring up GitHub Actions
      </LinkButton>
      <LinkButton href="https://github.com/acme/api/pull/412" external variant="quiet" icon="external">
        Pull request 412
      </LinkButton>
    </div>
  );
}

export function WithIcons() {
  return (
    <div className="row">
      <LinkButton to="/" icon="folder">
        Browse files
      </LinkButton>
      <LinkButton to="/" icon="arrow-right" variant="primary">
        Upload coverage
      </LinkButton>
      <LinkButton to="/" size="sm" icon="eye">
        View report
      </LinkButton>
    </div>
  );
}
