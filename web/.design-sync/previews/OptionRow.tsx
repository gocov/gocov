import { Avatar, Button, Chip, OptionRow } from "gocov-web";

export function AWorkspaceToRegister() {
  return (
    <OptionRow
      avatar={<Avatar kind="forge" forge="gitlab" />}
      name="acme-labs"
      status="4 repositories · not registered"
      action={<Button variant="primary">Register</Button>}
    />
  );
}

export function AlreadyRegistered() {
  return (
    <OptionRow
      avatar={<Avatar kind="forge" forge="bitbucket" />}
      name="acme"
      status="12 repositories · registered 6 Sep"
      action={<Button>Open</Button>}
    />
  );
}

export function AListToChooseFrom() {
  return (
    <div className="stack stack-1">
      <OptionRow
        avatar={<Avatar kind="forge" forge="github" />}
        name="acme"
        status="12 repositories · registered 6 Sep"
        action={<Button>Open</Button>}
      />
      <OptionRow
        avatar={<Avatar kind="forge" forge="github" />}
        name="acme-labs"
        status="4 repositories · not registered"
        action={<Button variant="primary">Register</Button>}
      />
      <OptionRow
        avatar={<Avatar kind="initial" name="omer" />}
        name="omer"
        status="Personal account · 2 repositories"
        action={<Chip tone="plain">Owner only</Chip>}
      />
    </div>
  );
}
