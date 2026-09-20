import { Mono, Pagination, SectionHeader, UploadsTable } from "gocov-web";

// Every row prints "3 hours ago", so the fixtures are measured from
// Date.now(): the grading sheet pins the page clock and the live card runs at
// the real time, and a literal timestamp would be wrong in one of them.
// Offsets stay under a day or well past two, so no row reads "yesterday"
// beside a genuinely two-day-old one.
const ago = (hours: number) => new Date(Date.now() - hours * 3_600_000).toISOString();

export function OnAllBranches() {
  return (
    <UploadsTable
      uploads={[
        { id: 412, sha: "a1b2c3d4e5f67890", branch: "main", pr_id: "", coverage: 82.3, gate_failed: false, at: ago(1) },
        { id: 411, sha: "9f2c41d8a7b30000", branch: "fix/upload-retry", pr_id: "128", coverage: 78.9, gate_failed: true, at: ago(4) },
        { id: 410, sha: "7c1de90fa2b41188", branch: "main", pr_id: "", coverage: 81.7, gate_failed: false, at: ago(20) },
        { id: 409, sha: "3ab77cc10de92244", branch: "feat/jacoco-parser", pr_id: "126", coverage: 74.2, gate_failed: false, at: ago(72) },
        { id: 408, sha: "5e0b9912aa47c630", branch: "main", pr_id: "", coverage: 80.4, gate_failed: false, at: ago(120) },
      ]}
    />
  );
}

export function OnOneBranch() {
  return (
    <section className="stack stack-1">
      <SectionHeader title="Uploads">
        <span className="muted small">
          On <Mono>main</Mono>, newest first
        </span>
      </SectionHeader>
      <UploadsTable
        uploads={[
          { id: 412, sha: "a1b2c3d4e5f67890", branch: "main", pr_id: "", coverage: 82.3, gate_failed: false, at: ago(1) },
          { id: 410, sha: "7c1de90fa2b41188", branch: "main", pr_id: "", coverage: 81.7, gate_failed: false, at: ago(20) },
          { id: 408, sha: "5e0b9912aa47c630", branch: "main", pr_id: "", coverage: 80.4, gate_failed: false, at: ago(120) },
        ]}
        empty="No uploads on main yet."
      />
      <Pagination newer={{ disabled: true }} older={{ to: "/" }} />
    </section>
  );
}

export function PullRequestReruns() {
  return (
    <UploadsTable
      uploads={[
        { id: 419, sha: "c04a7b18ee520931", branch: "fix/upload-retry", pr_id: "128", coverage: 80.1, gate_failed: false, at: ago(1) },
        { id: 417, sha: "1d93ff20b7a6c455", branch: "fix/upload-retry", pr_id: "128", coverage: 76.5, gate_failed: true, at: ago(3) },
        { id: 415, sha: "8b41c07de9f31200", branch: "fix/upload-retry", pr_id: "128", coverage: 61.0, gate_failed: true, at: ago(6) },
      ]}
    />
  );
}

export function NoUploadsYet() {
  return <UploadsTable uploads={[]} empty="No uploads on main yet." />;
}
