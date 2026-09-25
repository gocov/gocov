import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { Button, Chip, CoverageFigure, Mono, Notice, Spinner } from "@/components/atoms";
import { Card } from "@/components/molecules";
import { track } from "@/lib/analytics";
import type { SetupInfo, SetupStatus } from "@/lib/api/types";
import { forgeLabel, humanInt, shortSha } from "@/lib/format";
import { checklist, docsRecipe, snippetFilename } from "@/lib/snippets";
import { routes, server } from "@/lib/urls";
import { SnippetPanel, storedLanguage } from "./SnippetPanel";
import "./SetupChecklist.css";

/** How long a pipeline may plausibly take before silence is worth explaining. */
const HELP_AFTER_MS = 20_000;

/** The only timer in here: how long we have been listening, to the second. */
function useElapsed(since: number | null): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (since === null) return;
    setNow(Date.now());
    const tick = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(tick);
  }, [since]);
  return since === null ? 0 : now - since;
}

/** Fires an event the first time `when` becomes true, and never again. */
function useFiredOnce(when: boolean, event: string, props: Record<string, string>) {
  const fired = useRef(false);
  const latest = useRef(props);
  latest.current = props;
  useEffect(() => {
    if (!when || fired.current) return;
    fired.current = true;
    track(event, latest.current);
  }, [when, event]);
}

interface Props {
  info: SetupInfo;
  /** The polled status; fresher than `info.status`. */
  status: SetupStatus;
  /** When the snippet was first copied (epoch ms); null = not copied yet. */
  listeningSince: number | null;
  onReveal: () => Promise<string>;
  onCopied: () => void;
  /** Put the card away — offered only once coverage is flowing. */
  onDismiss: () => void;
}

/**
 * Setup as the dashboard's own state rather than a wizard, and as a card
 * rather than a checklist: setup asks exactly one thing of someone, so the
 * body holds that one thing. What the server already knows — the workspace
 * is there and you are in it — is a chip in the header, and the wait for
 * CI is the footer's line, because neither is a step anyone can take.
 * Nothing here is a dead end — the card is on a page that already works,
 * so leaving it is free and coming back costs nothing.
 *
 * It replaced a three-step wizard for a reason worth keeping in mind before
 * reshaping it. The work of setup happens in the user's repository and CI,
 * and takes longer than any in-page step; the wizard pretended otherwise,
 * and its waiting screen could only offer "Back". The first outside signup
 * (September 2026) reached it without ever copying the snippet and left.
 * Hence: Copy is the one primary action and what starts the wait; after
 * HELP_AFTER_MS of silence the card says what to check for this forge and
 * auth mode; and whether the card shows at all is decided from the server's
 * state (the dashboard page), never from browser storage.
 */
export function SetupChecklist({ info, status, listeningSince, onReveal, onCopied, onDismiss }: Props) {
  const [shown, setShown] = useState(false);
  const { forge, prefix } = info.workspace;
  const forgeName = forgeLabel(forge);
  const auth = info.tokenless ? "oidc" : "token";
  const events = { forge, auth, language: storedLanguage() };

  const copied = listeningSince !== null;
  const elapsed = useElapsed(listeningSince);
  const report = status.first_report;
  const needHelp = copied && report === null && elapsed >= HELP_AFTER_MS;

  useFiredOnce(report !== null, "first_upload_received", events);
  useFiredOnce(needHelp, "first_upload_help_shown", events);

  if (report !== null) {
    const { repo, branch, sha, coverage, covered_stmts: covered, total_stmts: total } = report;
    const offerReporting = info.reporting.available && info.reporting.state !== "on";
    return (
      <Card className="SetupChecklist">
        <Card.Header title="Coverage is flowing" />
        <Card.Body>
          <div className="stack">
            <div className="SetupChecklist__payoff">
              <CoverageFigure value={coverage} size="lg" />
              <p className="muted">
                First report from <Link to={routes.repo(repo.forge, repo.slug)}>{repo.slug}</Link> on{" "}
                <Mono>{branch}</Mono> &middot; <Mono>{shortSha(sha)}</Mono> &middot; {humanInt(covered)} of{" "}
                {humanInt(total)} statements
              </p>
            </div>
            {status.reports_posted !== "" ? (
              <Notice tone="good">{status.reports_posted}</Notice>
            ) : (
              offerReporting && <Notice>Nothing was posted back to {forgeName} yet.</Notice>
            )}
            <div className="stack stack-1">
              <h3 className="SetupChecklist__title">Next</h3>
              <div className="row row-2">
                <Link to={routes.workspace(forge, prefix)} onClick={() => track("set_gate_clicked", events)}>
                  Set a coverage gate
                </Link>
                {offerReporting && <Link to={routes.workspace(forge, prefix)}>Turn on reporting</Link>}
              </div>
            </div>
          </div>
        </Card.Body>
        <Card.Footer>
          <Button onClick={onDismiss}>Done</Button>
          <span className="muted small">This card goes away; the workspace settings keep everything in it.</span>
        </Card.Footer>
      </Card>
    );
  }

  const showSnippet = !copied || shown;

  return (
    <Card className="SetupChecklist">
      <Card.Header title="Set up coverage" actions={<Chip tone="good">Workspace {prefix} ready</Chip>} />
      <Card.Body>
        <div className="stack">
          {showSnippet ? (
            <SnippetPanel info={info} onReveal={onReveal} onCopied={onCopied} />
          ) : (
            <div className="row">
              <span className="muted small">
                Snippet copied &mdash; it goes in <Mono>{snippetFilename(forge)}</Mono>.
              </span>
              <Button size="sm" variant="quiet" onClick={() => setShown(true)}>
                Show snippet
              </Button>
            </div>
          )}
          {copied && shown && (
            <div className="row">
              <Button size="sm" variant="quiet" onClick={() => setShown(false)}>
                Hide snippet
              </Button>
            </div>
          )}
          {/* The answer to the footer's silence, and it needs room the footer
              has not got: the three things to check live in the body. */}
          {needHelp && (
            <Notice>
              <div className="stack stack-1">
                <p>Nothing yet &mdash; that is normal while a pipeline runs. If it has finished, check:</p>
                <ul className="SetupChecklist__help">
                  {checklist(forge, info.tokenless).map((item) => (
                    <li key={item}>{item}</li>
                  ))}
                </ul>
                <p>
                  <a href={server.docs(docsRecipe(forge))} target="_blank" rel="noopener">
                    The {forgeName} recipe in the docs
                  </a>
                </p>
              </div>
            </Notice>
          )}
        </div>
      </Card.Body>
      <Card.Footer>
        {copied ? (
          <>
            <span className="row" role="status">
              <Spinner label={null} />
              Listening for the first upload from {prefix}&hellip;
            </span>
            <span className="spacer" />
            <span>You can leave this page &mdash; gocov keeps listening, and the card will be here.</span>
          </>
        ) : (
          <span>The next pipeline run that reaches the upload step registers the repository.</span>
        )}
      </Card.Footer>
    </Card>
  );
}
