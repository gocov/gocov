import { LoginCard } from "gocov-web";

const providers = [
  { name: "github" as const, label: "GitHub" },
  { name: "gitlab" as const, label: "GitLab" },
  { name: "bitbucket" as const, label: "Bitbucket" },
];

export function EveryForgeOffered() {
  return <LoginCard info={{ hosted: false, providers, tracked_workspaces: [] }} next="/" error={null} />;
}

export function AHostedInstance() {
  return (
    <LoginCard
      info={{ hosted: true, providers, tracked_workspaces: [] }}
      next="/repos/github/acme/api"
      error={null}
    />
  );
}

export function OneForgeOnly() {
  return (
    <LoginCard
      info={{ hosted: false, providers: [providers[0]], tracked_workspaces: [] }}
      next="/"
      error={null}
    />
  );
}

export function AccountNotAllowed() {
  return (
    <LoginCard
      info={{
        hosted: false,
        providers,
        tracked_workspaces: [
          { name: "acme", forge: "github" },
          { name: "acme-labs", forge: "github" },
          { name: "beta", forge: "gitlab" },
        ],
      }}
      next="/"
      error="denied"
    />
  );
}

export function SignInDidNotComplete() {
  return <LoginCard info={{ hosted: true, providers, tracked_workspaces: [] }} next="/" error="failed" />;
}

export function NoProviderConfigured() {
  return <LoginCard info={{ hosted: false, providers: [], tracked_workspaces: [] }} next="/" error={null} />;
}
