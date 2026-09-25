import { Avatar, Button, LinkButton, Mono, Notice } from "@/components/atoms";
import { Card, EmptyState, OptionRow, PageHeader } from "@/components/molecules";
import type { OnboardingInfo, OnboardingRow } from "@/lib/api/types";
import { forgeLabel, plural } from "@/lib/format";
import { routes, server } from "@/lib/urls";
import "./WorkspacePicker.css";

/**
 * Where gocov will live. Two faces of the same choice: on GitHub the
 * workspace is created by installing the app, everywhere else it is picked
 * from the memberships the forge reported at sign-in.
 *
 * Analytics rides in as `onEvent` so the organism stays pure.
 */
export interface WorkspacePickerProps {
  info: OnboardingInfo;
  /** The prefix being registered right now; every other action waits. */
  busy: string | null;
  onRegister: (prefix: string) => void;
  onEvent?: (event: string, props?: Record<string, unknown>) => void;
}

/** GitLab calls them groups. */
const nounFor = (forge: string) => (forge === "gitlab" ? "group" : "workspace");

/** Free ones first: the row you came to press sits at the top. */
const rank: Record<OnboardingRow["state"], number> = {
  available: 0,
  registered: 1,
  member: 2,
  unowned: 3,
};

function statusFor(row: OnboardingRow, noun: string): string {
  switch (row.state) {
    case "available":
      return "Not set up yet";
    case "registered":
      return "Registered — join to start uploading";
    case "member":
      return "Registered — you’re a member";
    case "unowned":
      return `Creating it takes an owner of the ${noun} — ask one to sign in and set it up.`;
    default:
      return "Unavailable";
  }
}

export function WorkspacePicker({ info, busy, onRegister, onEvent }: WorkspacePickerProps) {
  const noun = nounFor(info.forge);
  const acting = busy !== null;

  if (info.mode === "install") {
    return (
      <div className="WorkspacePicker stack">
        <PageHeader
          title="Install gocov on your GitHub organization"
          meta="GitHub’s approval screen is where you pick the organization and the repositories gocov can see."
        />
        <Card>
          <Card.Body>
            {info.install_url === "" ? (
              <Notice tone="warn">
                The GitHub App is not configured on this instance, so there is nothing to install yet.
              </Notice>
            ) : (
              <div className="stack">
                <div>
                  <LinkButton
                    variant="primary"
                    href={info.install_url}
                    onClick={() => onEvent?.("install_app_clicked", { forge: info.forge })}
                  >
                    Install the gocov app
                  </LinkButton>
                </div>
                <p className="muted small">
                  gocov never needs your source code &mdash; only the coverage reports you upload.
                </p>
              </div>
            )}
          </Card.Body>
        </Card>
      </div>
    );
  }

  function action(row: OnboardingRow) {
    if (row.state === "member") {
      return <LinkButton to={routes.dashboard(`${info.forge}/${row.prefix}`)}>Open</LinkButton>;
    }
    if (row.state !== "available" && row.state !== "registered") return undefined;
    const create = row.state === "available";
    return (
      <Button
        variant={create ? "primary" : "default"}
        disabled={acting}
        loading={busy === row.prefix}
        onClick={() => {
          onEvent?.("register_workspace_clicked", { forge: info.forge });
          onRegister(row.prefix);
        }}
      >
        {busy !== row.prefix ? (create ? "Create" : "Join") : create ? "Creating…" : "Joining…"}
      </Button>
    );
  }

  const rows = [...info.rows].sort((a, b) => rank[a.state] - rank[b.state]);

  return (
    <div className="WorkspacePicker stack">
      <PageHeader title={`Choose a ${noun}`} />
      <Card>
        <Card.Body>
          {rows.length === 0 ? (
            <EmptyState message={`Your ${forgeLabel(info.forge)} account reported no ${noun}s at sign-in.`} />
          ) : (
            <div className="WorkspacePicker__rows">
              {rows.map((row) => (
                <OptionRow
                  key={row.prefix}
                  avatar={<Avatar name={row.prefix} />}
                  name={<Mono>{row.prefix}</Mono>}
                  status={statusFor(row, noun)}
                  action={action(row)}
                />
              ))}
            </div>
          )}
        </Card.Body>
      </Card>
      <p className="WorkspacePicker__foot">
        Signed in as {info.account} &middot; {plural(info.membership_count, "membership")} read at sign-in. Joined one
        since? <a href={server.oauthStart(info.forge, routes.onboarding())}>Sign in again</a>
      </p>
    </div>
  );
}
