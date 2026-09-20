import { ReposTable } from "gocov-web";

interface Repo {
  forge: string;
  slug: string;
  name: string;
  coverage: number | null;
  delta: number | null;
  gate: string;
  stale: boolean;
  series: number[];
  uploaded_at: string | null;
}

/**
 * Upload times are relative, never literal: the capture pins the page clock
 * to 2024 while the published card renders at the real time, and only an
 * offset from Date.now() reads correctly under both.
 */
const ago = (hours: number) => new Date(Date.now() - hours * 3_600_000).toISOString();

const repo = (over: Partial<Repo> & { name: string }): Repo => ({
  forge: "github",
  slug: `acme/${over.name}`,
  coverage: 70,
  delta: null,
  gate: "pass",
  stale: false,
  series: [68, 68.4, 69.1, 69, 70.2, 70],
  uploaded_at: ago(20),
  ...over,
});

const workspace: Repo[] = [
  repo({
    name: "api",
    coverage: 41,
    delta: -2.4,
    gate: "fail",
    series: [48, 47.2, 45.5, 44, 43.1, 41],
    uploaded_at: ago(3),
  }),
  repo({ name: "tools", coverage: 66, gate: "none", series: [64, 64.8, 65.2, 66, 66, 66] }),
  repo({
    name: "web",
    coverage: 82.5,
    delta: 0.4,
    stale: true,
    series: [80, 80.6, 81.4, 82, 82.1, 82.5],
    uploaded_at: ago(13 * 24),
  }),
  repo({ name: "billing", coverage: 74, delta: 2.8, series: [69, 70.5, 71.2, 72.8, 73.4, 74], uploaded_at: ago(96) }),
  repo({ name: "docs", coverage: null, delta: null, gate: "none", series: [], uploaded_at: null }),
];

export function Workspace() {
  return <ReposTable repos={workspace as never} />;
}

export function AwaitingFirstUploads() {
  return (
    <ReposTable
      repos={
        [
          repo({ name: "api", coverage: null, gate: "none", series: [], uploaded_at: null }),
          repo({ name: "web", coverage: null, gate: "none", series: [], uploaded_at: null }),
        ] as never
      }
    />
  );
}

export function NoRepositories() {
  return <ReposTable repos={[]} />;
}
