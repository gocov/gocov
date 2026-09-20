import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import {
  AppShell,
  Card,
  Chip,
  CoverageBar,
  CoverageFigure,
  Delta,
  Mono,
  PageHeader,
  SectionHeader,
  StatRow,
  StatTile,
} from "gocov-web";

// The shell reads GET /api/ui/session and shows a different bar for each
// answer, so the cell decides what that answer is. The stub is installed once,
// at module scope, and answers from `session` — which each cell sets while it
// renders, before the shell's query fires on mount.
let session = { user: { display_name: "Ada Lovelace", email: "ada@acme.example" }, auth_enabled: true, hosted: false };

const realFetch = window.fetch.bind(window);
window.fetch = (input, init) => {
  const url = String(typeof input === "string" ? input : input.url);
  if (url.includes("/api/ui/session")) {
    return Promise.resolve(
      new Response(JSON.stringify(session), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
  }
  return realFetch(input, init);
};

/**
 * The shell renders the page through the router's `Outlet`, which a preview
 * card has no route for — so the cell portals its page into the shell's own
 * content column. Scoped to this cell's wrapper, so two cells never fill each
 * other's column.
 */
function Framed({ children }) {
  const host = useRef(null);
  const [main, setMain] = useState(null);
  useEffect(() => {
    setMain(host.current?.querySelector(".AppShell__main") ?? null);
  }, []);
  return (
    <div ref={host}>
      <AppShell />
      {main !== null && createPortal(children, main)}
    </div>
  );
}

function dashboard() {
  return (
    <div className="stack stack-3">
      <PageHeader title={<Mono>acme</Mono>} meta="12 repositories · 74.6% covered" />
      <StatRow>
        <StatTile label="Coverage" value={<CoverageFigure value={74.6} size="md" />} hint="statement-weighted" />
        <StatTile label="Gates passing" value="9/12" />
        <StatTile label="Stale" value="1" hint="no upload in 14 days" />
        <StatTile label="Reporting" value={<Chip tone="good">Connected</Chip>} hint="gocov[bot]" />
      </StatRow>
      <section className="stack stack-1">
        <SectionHeader title="Repositories">12 repositories · 1 stale</SectionHeader>
        <Card>
          <Card.Body flush>
            <table>
              <thead>
                <tr>
                  <th>Repository</th>
                  <th>Coverage</th>
                  <th>Δ</th>
                  <th>Gate</th>
                </tr>
              </thead>
              <tbody>
                <tr>
                  <td>
                    <Mono>acme/api</Mono>
                  </td>
                  <td>
                    <CoverageBar value={82.3} />
                  </td>
                  <td>
                    <Delta value={1.4} />
                  </td>
                  <td>
                    <Chip tone="good">Passing</Chip>
                  </td>
                </tr>
                <tr>
                  <td>
                    <Mono>acme/web</Mono>
                  </td>
                  <td>
                    <CoverageBar value={61.0} />
                  </td>
                  <td>
                    <Delta value={-2.2} />
                  </td>
                  <td>
                    <Chip tone="bad">Failing</Chip>
                  </td>
                </tr>
                <tr>
                  <td>
                    <Mono>acme/worker</Mono>
                  </td>
                  <td>
                    <CoverageBar value={74.0} />
                  </td>
                  <td>
                    <Delta value={null} />
                  </td>
                  <td>
                    <Chip>No gate</Chip>
                  </td>
                </tr>
              </tbody>
            </table>
          </Card.Body>
        </Card>
      </section>
    </div>
  );
}

export function SignedIn() {
  session = { user: { display_name: "Ada Lovelace", email: "ada@acme.example" }, auth_enabled: true, hosted: false };
  return <Framed>{dashboard()}</Framed>;
}

export function SignedOut() {
  session = { user: null, auth_enabled: true, hosted: true };
  return (
    <Framed>
      <div className="stack stack-3">
        <PageHeader
          title={<Mono>acme/api</Mono>}
          meta="Public reports · 82.3% on main"
          actions={<Chip tone="good">Gate passing</Chip>}
        />
        <Card>
          <Card.Header title="Latest report" />
          <Card.Body>
            <p>
              <Mono>a1b2c3d4e5f6</Mono> on <Mono>main</Mono> — 82.3% of 11,224 statements covered, 3 hours ago.
            </p>
          </Card.Body>
        </Card>
      </div>
    </Framed>
  );
}

export function OpenInstance() {
  session = { user: null, auth_enabled: false, hosted: false };
  return <Framed>{dashboard()}</Framed>;
}
