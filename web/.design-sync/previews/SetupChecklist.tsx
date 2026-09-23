import { SetupChecklist } from "gocov-web";

const waiting = { repo_count: 0, first_report: null, reports_posted: "" };

const info = {
  workspace: { forge: "github", prefix: "acme", forge_label: "GitHub" },
  owner: true,
  tokenless: true,
  connection_broken: false,
  base_url: "https://app.gocov.dev",
  server_implicit: true,
  gitlab_catalog: false,
  cli_version: "v0.25.0",
  token_masked: "gocov_live_••••••••3f2a",
  reporting: { available: true, state: "on", account: "", connect_url: "" },
  status: waiting,
};

const arrived = {
  repo_count: 1,
  first_report: {
    repo: { forge: "github", slug: "acme/api" },
    branch: "main",
    sha: "a1b2c3d4e5f60718",
    coverage: 81.25,
    covered_stmts: 1300,
    total_stmts: 1600,
  },
  reports_posted: "Commit status posted as gocov[bot].",
};

const reveal = async () => "gocov_live_9f2c41d8a7b3";
const noop = () => {};

/**
 * Not copied yet: the body is the snippet and Copy is the one move. What the
 * server already knows is the header's chip, and the footer says what happens
 * next rather than asking for anything.
 */
export function CopyTheSnippet() {
  return (
    <SetupChecklist
      info={info as never}
      status={waiting as never}
      listeningSince={null}
      onReveal={reveal}
      onCopied={noop}
      onDismiss={noop}
    />
  );
}

/**
 * Copied: the snippet folds to a single line and the footer starts listening.
 * The shortest the card ever gets, and the shape it holds for as long as CI
 * takes.
 */
export function Listening() {
  return (
    <SetupChecklist
      info={info as never}
      status={waiting as never}
      listeningSince={Date.now()}
      onReveal={reveal}
      onCopied={noop}
      onDismiss={noop}
    />
  );
}

/**
 * The same collapsed card after twenty seconds of silence, on the token recipe
 * rather than OIDC: the body gains the three things to check for that forge and
 * auth mode. The timer itself cannot be previewed, but backdating the copy
 * renders the state it lands in.
 */
export function NothingYet() {
  return (
    <SetupChecklist
      info={{ ...info, tokenless: false } as never}
      status={waiting as never}
      listeningSince={Date.now() - 30_000}
      onReveal={reveal}
      onCopied={noop}
      onDismiss={noop}
    />
  );
}

/** The payoff: the first report replaces the whole card. */
export function CoverageIsFlowing() {
  return (
    <SetupChecklist
      info={info as never}
      status={arrived as never}
      listeningSince={Date.now() - 45_000}
      onReveal={reveal}
      onCopied={noop}
      onDismiss={noop}
    />
  );
}
