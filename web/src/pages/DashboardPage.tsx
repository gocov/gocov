import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { Chip, CoverageFigure, LinkButton, Mono, Notice, type Tone } from "@/components/atoms";
import { Card, EmptyState, PageHeader, QueryBoundary, SectionHeader, StatRow, StatTile } from "@/components/molecules";
import { AttentionList } from "@/components/organisms/AttentionList";
import { ReposTable } from "@/components/organisms/ReposTable";
import { SetupChecklist } from "@/components/organisms/SetupChecklist";
import { WorkspaceSwitcher } from "@/components/organisms/WorkspaceSwitcher";
import { apiPost } from "@/lib/api/client";
import { dashboardQuery, setupQuery, setupStatusQuery, workspaceSettingsPath } from "@/lib/api/queries";
import type { Dashboard, DashStats, TokenReveal, WorkspaceGroup } from "@/lib/api/types";
import { pct, plural } from "@/lib/format";
import { useUrlNotice } from "@/lib/notice";
import { readStored, writeStored } from "@/lib/storage";
import { usePageTitle } from "@/lib/title";
import { routes } from "@/lib/urls";
import { attentionRows } from "@/lib/dashboard";

/** The codes the connect redirects carry, said the way this page can act on them. */
const connectNotices = {
  connect_failed:
    "Connecting to the forge did not complete. Nothing was changed — try again from the workspace settings.",
  // Said the same whether the workspace is someone else's or not there at all.
  connect_denied:
    "That workspace was not connected: your account is not a member of it here. Nothing was changed.",
};

/** How the forge connection reads in the stat row. */
const reportingStates: Record<DashStats["reporting"], { tone: Tone; label: string }> = {
  connected: { tone: "good", label: "Connected" },
  not_connected: { tone: "neutral", label: "Not connected" },
  broken: { tone: "bad", label: "Reconnect needed" },
};

/** Where "add a repository" goes: this workspace's snippet, or registration. */
const setupRoute = (ws: WorkspaceGroup) =>
  ws.tracked ? routes.workspaceSetup(ws.forge, ws.prefix) : routes.onboarding();

// The setup card remembers one thing, and only for this visit: when the wait
// for the first upload began, so a reload does not restart the 20-second clock.
// Whether the card shows at all is never a browser's to remember — the server
// knows whether the workspace has a report.
const listeningKey = (ws: WorkspaceGroup) => `gocov.setup.listening.${ws.forge}/${ws.prefix}`;

/**
 * Setup as part of the dashboard rather than a wizard elsewhere. It fetches
 * its own data and fails quietly: a workspace's repositories are worth more
 * than the card above them, so a broken setup query renders nothing at all.
 */
function SetupSection({ ws, hasReports }: { ws: WorkspaceGroup; hasReports: boolean }) {
  const queryClient = useQueryClient();
  // Decided once, on arrival: the card is for a workspace that had no report
  // when the viewer got here. If the report lands while they watch, the card
  // becomes the payoff; on any later visit the workspace simply has reports
  // and there is no card — nothing to store, nothing to come back.
  const [arrivedEmpty] = useState(!hasReports);
  const [dismissed, setDismissed] = useState(false);
  const [listeningSince, setListeningSince] = useState<number | null>(() => {
    const saved = Number(readStored("sessionStorage", listeningKey(ws)));
    return Number.isFinite(saved) && saved > 0 ? saved : null;
  });

  const wanted = arrivedEmpty && !dismissed;
  const setup = useQuery({ ...setupQuery(ws.forge, ws.prefix), enabled: wanted });
  const info = setup.data;

  const reported = info?.status.first_report ?? null;
  const status = useQuery({
    ...setupStatusQuery(ws.forge, ws.prefix),
    enabled: wanted && info !== undefined && reported === null,
    // Every three seconds until the report lands, then never again.
    refetchInterval: (query) => (query.state.data?.first_report ? false : 3000),
  });

  const live = status.data ?? info?.status ?? null;
  const firstReport = live?.first_report ?? null;

  // The dashboard was drawn before this repository existed; once its first
  // report lands, the table below should show it.
  const landed = firstReport !== null;
  useEffect(() => {
    if (landed) void queryClient.invalidateQueries({ queryKey: ["dashboard"] });
  }, [landed, queryClient]);

  if (!wanted || info === undefined || live === null) return null;
  // Nothing to celebrate and nothing to set up: an established workspace
  // whose card was put away.
  if (hasReports && firstReport === null) return null;

  return (
    <SetupChecklist
      info={info}
      status={live}
      listeningSince={listeningSince}
      onReveal={async () => {
        const { token } = await apiPost<TokenReveal>(workspaceSettingsPath(ws.forge, ws.prefix) + "/reveal-token");
        return token;
      }}
      onCopied={() => {
        if (listeningSince !== null) return;
        const now = Date.now();
        setListeningSince(now);
        writeStored("sessionStorage", listeningKey(ws), String(now));
      }}
      onDismiss={() => setDismissed(true)}
    />
  );
}

/** Nothing at all: no repositories, and no workspace to put one in. */
function NoWorkspace({ canOnboard }: { canOnboard: boolean }) {
  return (
    <div className="stack stack-3">
      <PageHeader title="Repositories" meta="No repositories yet." />
      <Card>
        <Card.Body>
          <EmptyState
            message={
              canOnboard ? (
                <>
                  No repositories yet. <Link to={routes.onboarding()}>Register a workspace</Link> for an upload token,
                  then push coverage from CI &mdash; repos register themselves on their first upload.
                </>
              ) : (
                "No repositories yet. Enable sign-in so a member can register a workspace and get an upload token; repos then register themselves on their first upload."
              )
            }
          />
        </Card.Body>
      </Card>
    </div>
  );
}

function Workspace({ data, current }: { data: Dashboard; current: WorkspaceGroup }) {
  const { stats, repos, attention, can_onboard: canOnboard } = data;
  const notices = attentionRows(attention);
  const reporting = reportingStates[stats.reporting];
  const setup = setupRoute(current);
  const hasReports = repos.some((r) => r.coverage !== null);

  return (
    <div className="stack stack-3">
      <PageHeader
        title={<WorkspaceSwitcher current={current} groups={data.switcher} canOnboard={canOnboard} />}
        meta={
          plural(current.repo_count, "repository", "repositories") +
          (stats.coverage === null ? "" : ` · ${pct(stats.coverage)} covered`)
        }
        actions={
          <>
            {current.tracked && (
              <LinkButton to={routes.workspace(current.forge, current.prefix)}>Workspace settings</LinkButton>
            )}
            {canOnboard && (
              <LinkButton variant="primary" to={setup}>
                Add a repository
              </LinkButton>
            )}
          </>
        }
      />

      {current.tracked && canOnboard && (
        // Keyed: switching workspace is a new arrival, with its own verdict.
        <SetupSection key={`${current.forge}/${current.prefix}`} ws={current} hasReports={hasReports} />
      )}

      <StatRow>
        <StatTile
          label="Workspace coverage"
          value={<CoverageFigure value={stats.coverage} size="md" />}
          hint={stats.coverage === null ? "no uploads yet" : "weighted by statements"}
        />
        <StatTile
          label="Gates passing"
          value={stats.gates_passing}
          hint={`of ${stats.gates_total} with a gate${stats.stale_count > 0 ? ` · ${stats.stale_count} stale` : ""}`}
        />
        <StatTile
          label="Reporting"
          value={<Chip tone={reporting.tone}>{reporting.label}</Chip>}
          hint={stats.reporting_as === "" ? undefined : <>as <Mono>{stats.reporting_as}</Mono></>}
        />
      </StatRow>

      {notices.length > 0 && (
        <section className="stack stack-1">
          <SectionHeader title="Needs attention">{plural(notices.length, "thing")}</SectionHeader>
          <AttentionList rows={notices} />
        </section>
      )}

      <section className="stack stack-1">
        <SectionHeader title="Repositories" />
        {repos.length > 0 ? (
          <ReposTable repos={repos} />
        ) : (
          <Card>
            <Card.Body>
              <EmptyState
                message={
                  canOnboard
                    ? "No repositories in this workspace yet. Push coverage from CI and repos register themselves on their first upload."
                    : "No repositories yet. Enable sign-in so a member can register a workspace and get an upload token; repos then register themselves on their first upload."
                }
                action={canOnboard ? <LinkButton to={setup}>Setup instructions</LinkButton> : undefined}
              />
            </Card.Body>
          </Card>
        )}
      </section>
    </div>
  );
}

function DashboardView({ data }: { data: Dashboard }) {
  const navigate = useNavigate();
  // A hosted user with no workspace has nothing to look at: choosing one is
  // a page in this app, so go there without leaving it.
  useEffect(() => {
    if (data.needs_onboarding) void navigate(routes.onboarding(), { replace: true });
  }, [data.needs_onboarding, navigate]);

  if (data.needs_onboarding) return null;
  if (data.current === null) return <NoWorkspace canOnboard={data.can_onboard} />;
  return <Workspace data={data} current={data.current} />;
}

/** The index route: one workspace's repositories, chosen with ?ws=forge/prefix. */
export default function DashboardPage() {
  const [params] = useSearchParams();
  // Where the server sends the browser back after an install or a grant. A
  // consent that failed before it named a workspace lands here, with no
  // settings page of its own to return to.
  const notice = useUrlNotice({ codes: connectNotices });
  const query = useQuery(dashboardQuery(params.get("ws") ?? ""));
  usePageTitle("repositories");
  return (
    <div className="stack stack-3">
      {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}
      <QueryBoundary query={query}>{(data) => <DashboardView data={data} />}</QueryBoundary>
    </div>
  );
}
