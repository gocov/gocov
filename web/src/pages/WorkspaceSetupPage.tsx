import { useQuery } from "@tanstack/react-query";
import { useRef } from "react";
import { Link, useParams } from "react-router";
import { Notice } from "@/components/atoms";
import { Breadcrumbs, Card, PageHeader, QueryBoundary } from "@/components/molecules";
import { SnippetPanel } from "@/components/organisms/SnippetPanel";
import { apiPost } from "@/lib/api/client";
import { setupQuery, setupStatusQuery, workspaceSettingsPath } from "@/lib/api/queries";
import { track } from "@/lib/analytics";
import type { SetupInfo, TokenReveal } from "@/lib/api/types";
import { usePageTitle } from "@/lib/title";
import { routes } from "@/lib/urls";
import "./WorkspaceSetupPage.css";

function Setup({ info }: { info: SetupInfo }) {
  const { forge, prefix } = info.workspace;
  const ws = `${forge}/${prefix}`;
  // What the workspace held when this page opened: anything beyond it
  // arrived while it was open, which is the only news this page has.
  const before = useRef(info.status.repo_count);

  const status = useQuery({
    ...setupStatusQuery(forge, prefix),
    refetchInterval: (query) => ((query.state.data?.repo_count ?? 0) > before.current ? false : 3000),
  });
  const received = (status.data?.repo_count ?? info.status.repo_count) > before.current;

  return (
    <div className="stack stack-3">
      <PageHeader
        breadcrumbs={
          <Breadcrumbs
            items={[
              { label: "Repositories", to: routes.dashboard() },
              { label: prefix, to: routes.dashboard(ws) },
              { label: "Add a repository" },
            ]}
          />
        }
        title="Add a repository"
        meta={`Every repository in ${prefix} uses the same snippet — each one registers itself on its first upload.`}
      />

      <Card>
        <Card.Body>
          <SnippetPanel
            info={info}
            onReveal={async () => {
              const { token } = await apiPost<TokenReveal>(workspaceSettingsPath(forge, prefix) + "/reveal-token");
              return token;
            }}
          />
        </Card.Body>
      </Card>

      <div className="WorkspaceSetup__listening">
        {received ? (
          <Notice tone="good">
            New repository received.{" "}
            <Link to={routes.dashboard(ws)} onClick={() => track("open_dashboard_clicked", { forge })}>
              Open the dashboard
            </Link>
          </Notice>
        ) : (
          <Notice busy>Listening for an upload from {prefix}&hellip; you can leave this page.</Notice>
        )}
      </div>
    </div>
  );
}

/** "Add a repository": the same snippet, independent of first-run setup. */
export default function WorkspaceSetupPage() {
  const { forge = "", prefix = "" } = useParams();
  const query = useQuery(setupQuery(forge, prefix));
  usePageTitle("add a repository");
  return <QueryBoundary query={query}>{(info) => <Setup info={info} />}</QueryBoundary>;
}
