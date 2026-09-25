import { useState } from "react";
import type { Forge, WorkspaceSettings } from "@/lib/api/types";
import { appAccount, forgeLabel, plural } from "@/lib/format";
import { Avatar, Button, Chip, LinkButton, Notice } from "@/components/atoms";
import { Card, ConfirmDialog, IdentityRow } from "@/components/molecules";

/**
 * The three forges differ in words, not in shape: what gocov posts, and
 * whether the connection is an app install (a bot posts) or an OAuth grant
 * (the connecting account posts).
 */
interface ForgeWords {
  /** What gets posted back, mid-sentence: "commit statuses and …". */
  posts: string;
  /** GitHub: an app install rather than a grant from an account. */
  install: boolean;
  /** The label of the connect button in each state; "" = no button. */
  action: { on: string; off: string; broken: string };
}

const grant = (posts: string): ForgeWords => ({
  posts,
  install: false,
  action: { on: "", off: "Grant write access", broken: "Grant write access again" },
});

const forgeWords: Record<string, ForgeWords> = {
  github: {
    posts: "commit statuses, check runs and pull-request comments",
    install: true,
    action: { on: "Manage on GitHub", off: "Install the gocov app", broken: "Reinstall the app" },
  },
  bitbucket: grant("build statuses and pull-request comments"),
  gitlab: grant("commit statuses and merge-request comments"),
};

const sentenceCase = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

interface Props {
  forge: Forge;
  reporting: WorkspaceSettings["reporting"];
  owner: boolean;
  repoCount: number;
  onDisconnect: () => void;
}

/**
 * Whether gocov posts back to the forge, who it posts as, and the one
 * move that changes that. Renders nothing where the deployment has no way
 * to connect this forge at all.
 */
export function ReportingCard({ forge, reporting, owner, repoCount, onDisconnect }: Props) {
  const [confirming, setConfirming] = useState(false);
  const forgeName = forgeLabel(forge);
  if (!reporting.available) return null;

  const words = forgeWords[forge] ?? grant("commit statuses and merge-request comments");
  const { state, account, connect_url: connectUrl } = reporting;
  const repos = repoCount > 0 ? ` · ${plural(repoCount, "repository", "repositories")}` : "";

  const chip =
    state === "on" ? (
      <Chip tone="good">Connected</Chip>
    ) : state === "broken" ? (
      <Chip tone="bad">Reconnect needed</Chip>
    ) : (
      <Chip tone="plain">Not connected</Chip>
    );

  const explanation =
    state === "on"
      ? words.install
        ? `${sentenceCase(words.posts)} are posted through the app install. Nothing to manage here — permissions and repository access are changed on ${forgeName}.`
        : `${sentenceCase(words.posts)} are posted with the write access you granted. They appear under the connecting account — ${forgeName} has no bot identity for apps like gocov.`
      : state === "broken"
        ? `Coverage still uploads and shows in gocov; only posting back has stopped. ${words.install ? "Reinstalling the app restores it." : "Granting access again restores it."}`
        : words.install
          ? `Coverage still uploads and shows in gocov. Installing the app is what lets gocov post ${words.posts} back to ${forgeName}.`
          : `Coverage still uploads and shows in gocov. Granting write access is what lets gocov post ${words.posts}, under your own account.`;

  const actionLabel = words.action[state];

  return (
    <Card>
      <Card.Header title={`Reporting to ${forgeName}`} actions={chip} />
      <Card.Body>
        <div className="stack">
          {state === "broken" && (
            <Notice tone="bad">
              {forge === "github"
                ? "The app install stopped working — it was removed or suspended on GitHub."
                : `The grant stopped working — it was revoked on ${forgeName}, or the account that connected it lost access.`}{" "}
              Nothing has been posted back since it broke.
            </Notice>
          )}
          <p>{explanation}</p>
          {state === "on" &&
            (words.install ? (
              <IdentityRow
                avatar={<Avatar kind="bot" />}
                id={appAccount}
                description={`Posting through the app install${repos}`}
                chip={<Chip tone="plain">App install</Chip>}
              />
            ) : (
              <IdentityRow
                avatar={<Avatar kind="person" />}
                id={`@${account}`}
                description="Statuses and comments are posted as this account"
                chip={<Chip tone="plain">Your account</Chip>}
              />
            ))}
          {state === "broken" && (
            <IdentityRow
              avatar={<Avatar kind={account === "" ? "bot" : "person"} />}
              id={account === "" ? appAccount : `@${account}`}
              description={words.install ? `Install removed or suspended on ${forgeName}` : "Grant revoked"}
              chip={<Chip tone="bad">Inactive</Chip>}
            />
          )}
        </div>
      </Card.Body>
      <Card.Footer>
        {!owner ? (
          <span>Connecting and disconnecting are a workspace owner&rsquo;s moves.</span>
        ) : (
          <>
            {actionLabel !== "" && connectUrl !== "" && (
              <LinkButton
                href={connectUrl}
                variant={state === "on" ? "default" : "primary"}
                external={words.install}
                icon={words.install ? "external" : undefined}
              >
                {actionLabel}
              </LinkButton>
            )}
            <span className="spacer" />
            {state === "off" && <span>Optional — uploads work without it.</span>}
            {state !== "off" && (
              <Button variant={state === "on" ? "danger" : "default"} onClick={() => setConfirming(true)}>
                Disconnect
              </Button>
            )}
          </>
        )}
      </Card.Footer>
      {owner && state !== "off" && (
        <ConfirmDialog
          open={confirming}
          danger
          title="Disconnect?"
          confirmLabel="Disconnect"
          onConfirm={() => {
            setConfirming(false);
            onDisconnect();
          }}
          onCancel={() => setConfirming(false)}
        >
          Statuses and comments are then skipped for this workspace.
        </ConfirmDialog>
      )}
    </Card>
  );
}
