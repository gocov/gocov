import { useEffect, useRef } from "react";
import { LinkButton, PageHeader, WorkspaceSwitcher } from "gocov-web";

const group = (over) => ({
  forge: "github",
  prefix: "acme",
  repo_count: 12,
  coverage: 74.6,
  current: false,
  tracked: true,
  ...over,
});

const acme = group({ current: true });
const labs = group({ forge: "gitlab", prefix: "acme-labs", repo_count: 4, coverage: null });
const platform = group({ forge: "bitbucket", prefix: "acme-platform", repo_count: 7, coverage: 61.2 });
const globex = group({ prefix: "globex", repo_count: 3, coverage: 88.1 });

/**
 * The popover only opens on a click, so the cell presses the trigger once it
 * has mounted. The component closes on a document `mousedown`, which a
 * synthetic click never fires, so it stays open for the capture. The padding
 * is the room the absolutely-positioned popover needs inside its cell.
 */
function Opened({ children }) {
  const host = useRef(null);
  useEffect(() => {
    host.current?.querySelector(".WorkspaceSwitcher__trigger")?.click();
  }, []);
  return (
    <div ref={host} style={{ paddingBottom: "calc(var(--space-3) * 8)" }}>
      {children}
    </div>
  );
}

export function AsThePageTitle() {
  return (
    <PageHeader
      title={<WorkspaceSwitcher current={acme} groups={[acme, labs, platform]} canOnboard />}
      meta="12 repositories · 74.6% covered"
      actions={
        <>
          <LinkButton to="/">Workspace settings</LinkButton>
          <LinkButton variant="primary" to="/">
            Add a repository
          </LinkButton>
        </>
      }
    />
  );
}

export function TheOnlyWorkspace() {
  return <PageHeader title={<WorkspaceSwitcher current={acme} groups={[acme]} canOnboard />} meta="12 repositories · 74.6% covered" />;
}

export function Open() {
  return (
    <Opened>
      <PageHeader
        title={<WorkspaceSwitcher current={acme} groups={[acme, labs, platform, globex]} canOnboard />}
        meta="12 repositories · 74.6% covered"
      />
    </Opened>
  );
}

export function OpenAndSearchable() {
  const many = [
    acme,
    labs,
    platform,
    globex,
    group({ prefix: "acme-infra", repo_count: 5, coverage: 69.3 }),
    group({ forge: "gitlab", prefix: "acme-data", repo_count: 2, coverage: 55.8 }),
    group({ prefix: "acme-docs", repo_count: 1, coverage: null }),
    group({ forge: "bitbucket", prefix: "initech", repo_count: 6, coverage: 80.0 }),
  ];
  return (
    <Opened>
      <PageHeader title={<WorkspaceSwitcher current={acme} groups={many} canOnboard />} meta="12 repositories · 74.6% covered" />
    </Opened>
  );
}
