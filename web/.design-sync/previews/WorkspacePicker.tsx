import { WorkspacePicker } from "gocov-web";

const bitbucket = {
  forge: "bitbucket",
  account: "Ada Lovelace",
  mode: "pick",
  install_url: "",
  rows: [
    { prefix: "acme", state: "available" },
    { prefix: "acme-labs", state: "registered" },
    { prefix: "acme-platform", state: "member" },
    { prefix: "globex", state: "unowned" },
  ],
  membership_count: 4,
};

export function ChooseAWorkspace() {
  return <WorkspacePicker info={bitbucket} busy={null} onRegister={() => {}} />;
}

export function CreatingOne() {
  return <WorkspacePicker info={bitbucket} busy="acme" onRegister={() => {}} />;
}

export function GitLabGroups() {
  return (
    <WorkspacePicker
      info={{
        forge: "gitlab",
        account: "Ada Lovelace",
        mode: "pick",
        install_url: "",
        rows: [
          { prefix: "acme/backend", state: "available" },
          { prefix: "acme/tooling", state: "member" },
          { prefix: "globex", state: "unowned" },
        ],
        membership_count: 3,
      }}
      busy={null}
      onRegister={() => {}}
    />
  );
}

export function InstallOnGitHub() {
  return (
    <WorkspacePicker
      info={{
        forge: "github",
        account: "Ada Lovelace",
        mode: "install",
        install_url: "https://github.com/apps/gocov/installations/new",
        rows: [],
        membership_count: 0,
      }}
      busy={null}
      onRegister={() => {}}
    />
  );
}

export function TheAppIsNotConfigured() {
  return (
    <WorkspacePicker
      info={{
        forge: "github",
        account: "Ada Lovelace",
        mode: "install",
        install_url: "",
        rows: [],
        membership_count: 0,
      }}
      busy={null}
      onRegister={() => {}}
    />
  );
}

export function NoMemberships() {
  return (
    <WorkspacePicker
      info={{
        forge: "bitbucket",
        account: "Ada Lovelace",
        mode: "pick",
        install_url: "",
        rows: [],
        membership_count: 0,
      }}
      busy={null}
      onRegister={() => {}}
    />
  );
}
