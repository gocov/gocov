import { SnippetPanel } from "gocov-web";

const info = {
  workspace: { forge: "github", prefix: "acme" },
  owner: true,
  tokenless: true,
  connection_broken: false,
  base_url: "https://app.gocov.dev",
  server_implicit: true,
  gitlab_catalog: false,
  cli_version: "v0.25.0",
  token_masked: "gocov_live_••••••••3f2a",
  reporting: { available: true, state: "on", account: "", connect_url: "" },
  status: { repo_count: 0, first_report: null, reports_posted: "" },
};

const reveal = async () => "gocov_live_9f2c41d8a7b3";

/** The connected workspace: an identity token, so the snippet needs no secret. */
export function GitHubTokenless() {
  return <SnippetPanel info={info as never} onReveal={reveal} />;
}

/** No connection to mint an identity token: the secret comes up front. */
export function GitHubWithToken() {
  return <SnippetPanel info={{ ...info, tokenless: false } as never} onReveal={reveal} />;
}

/** GitLab, where the CI/CD Catalog component replaces the raw download. */
export function GitLabCatalog() {
  return (
    <SnippetPanel
      info={
        {
          ...info,
          workspace: { forge: "gitlab", prefix: "acme/platform" },
          gitlab_catalog: true,
        } as never
      }
      onReveal={reveal}
    />
  );
}

/** A self-hosted instance hands over GOCOV_SERVER alongside the token. */
export function SelfHosted() {
  return (
    <SnippetPanel
      info={{ ...info, tokenless: false, server_implicit: false, base_url: "https://cov.acme.dev" } as never}
      onReveal={reveal}
    />
  );
}

/** A member may not see the token; the snippet is still the whole point. */
export function MemberWithoutToken() {
  return (
    <SnippetPanel
      info={{ ...info, tokenless: false, owner: false, token_masked: null } as never}
      onReveal={reveal}
    />
  );
}

/** The workspace connection broke, so jobs need the token again. */
export function ConnectionBroken() {
  return (
    <SnippetPanel info={{ ...info, tokenless: false, connection_broken: true } as never} onReveal={reveal} />
  );
}
