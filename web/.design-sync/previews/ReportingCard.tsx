import { ReportingCard } from "gocov-web";

const noop = () => {};

export function GitHubConnected() {
  return (
    <ReportingCard
      forge="github"
      reporting={{
        available: true,
        state: "on",
        account: "",
        connect_url: "https://github.com/apps/gocov/installations/new",
      }}
      owner
      repoCount={8}
      onDisconnect={noop}
    />
  );
}

export function GitLabNotConnected() {
  return (
    <ReportingCard
      forge="gitlab"
      reporting={{
        available: true,
        state: "off",
        account: "",
        connect_url: "/workspace-settings/gitlab/acme/connect",
      }}
      owner
      repoCount={3}
      onDisconnect={noop}
    />
  );
}

export function BitbucketBroken() {
  return (
    <ReportingCard
      forge="bitbucket"
      reporting={{
        available: true,
        state: "broken",
        account: "omer",
        connect_url: "/workspace-settings/bitbucket/acme/connect",
      }}
      owner
      repoCount={5}
      onDisconnect={noop}
    />
  );
}

export function MemberView() {
  return (
    <ReportingCard
      forge="github"
      reporting={{
        available: true,
        state: "on",
        account: "",
        connect_url: "https://github.com/apps/gocov/installations/new",
      }}
      owner={false}
      repoCount={8}
      onDisconnect={noop}
    />
  );
}
