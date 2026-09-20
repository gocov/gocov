import { Avatar, Chip, IdentityRow, Mono } from "gocov-web";

export function ABot() {
  return (
    <IdentityRow
      avatar={<Avatar kind="bot" />}
      id="gocov[bot]"
      description="Posting through the app install · 8 repositories"
      chip={<Chip tone="plain">App install</Chip>}
    />
  );
}

export function APersonWithABrokenGrant() {
  return (
    <IdentityRow
      avatar={<Avatar kind="person" />}
      id="@omer"
      description="Grant revoked on GitLab"
      chip={<Chip tone="bad">Inactive</Chip>}
    />
  );
}

export function AWorkspaceOwner() {
  return (
    <IdentityRow
      avatar={<Avatar kind="forge" forge="github" />}
      id={<Mono>acme</Mono>}
      description="Statuses and pull-request comments are posted as this account"
      chip={<Chip tone="good">Connected</Chip>}
    />
  );
}

export function AStackOfRows() {
  return (
    <div className="stack stack-1">
      <IdentityRow
        avatar={<Avatar kind="person" />}
        id="@omer"
        description="Owner · signed in with GitHub"
        chip={<Chip tone="plain">Owner</Chip>}
      />
      <IdentityRow
        avatar={<Avatar kind="person" />}
        id="@deniz"
        description="Member · last seen 2 days ago"
        chip={<Chip tone="plain">Member</Chip>}
      />
      <IdentityRow
        avatar={<Avatar kind="bot" />}
        id="gocov[bot]"
        description="Posts statuses and pull-request comments"
        chip={<Chip tone="good">Active</Chip>}
      />
    </div>
  );
}
