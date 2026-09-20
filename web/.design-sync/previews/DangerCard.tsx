import { DangerCard, InlineCode } from "gocov-web";

export function DeleteWorkspace() {
  return (
    <DangerCard
      title="Delete workspace"
      actionLabel="Delete this workspace"
      hint="This cannot be undone."
      ownerOnlyHint="Only a workspace owner can delete it."
      owner
      confirmText="Delete acme and all of its coverage data? This cannot be undone."
      onConfirm={async () => {}}
    >
      <p>
        Removes <InlineCode>acme</InlineCode>, its 12 repositories and every coverage report gocov holds for them.
        Uploads with this token start failing immediately. Nothing is changed on GitHub.
      </p>
    </DangerCard>
  );
}

export function RemoveRepository() {
  return (
    <DangerCard
      title="Remove repository"
      actionLabel="Remove this repository"
      hint="This cannot be undone."
      ownerOnlyHint="Only a workspace owner can remove it."
      owner
      confirmText="Remove acme/api and all of its coverage data? This cannot be undone."
      onConfirm={async () => {}}
    >
      <p>
        Removes <InlineCode>acme/api</InlineCode> from gocov along with its uploads and every report behind them.
        Uploads with its token start failing immediately. Nothing is changed on the forge — the repository itself, and
        any branch protection referring to the gocov check, stay as they are.
      </p>
    </DangerCard>
  );
}

export function ForAMember() {
  return (
    <DangerCard
      title="Remove repository"
      actionLabel="Remove this repository"
      hint="This cannot be undone."
      ownerOnlyHint="Only a workspace owner can remove it."
      owner={false}
      confirmText="Remove acme/api and all of its coverage data? This cannot be undone."
      onConfirm={async () => {}}
    >
      <p>
        Removes <InlineCode>acme/api</InlineCode> from gocov along with its uploads and every report behind them.
        Uploads with its token start failing immediately.
      </p>
    </DangerCard>
  );
}
