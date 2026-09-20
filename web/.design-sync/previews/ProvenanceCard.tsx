import { ProvenanceCard } from "gocov-web";

/**
 * Upload times are relative to whatever clock the card renders on, so they
 * read the same on a grading screenshot and on the live card.
 */
const ago = (hours: number) => new Date(Date.now() - hours * 3_600_000).toISOString();

export function HowAnUploadArrived() {
  return (
    <ProvenanceCard
      downloadUrl="/uploads/412/profile"
      provenance={{
        received_at: ago(2),
        profile_name: "coverage.out",
        profile_size: "184 kB",
        format: "go",
        ci_label: "GitHub Actions #2184",
        ci_run_url: "https://github.com/acme/api/actions/runs/2184",
        uploader: "gocov-action v1.17.0",
        uploader_kind: "action",
        part: "unit",
        parts_note: "3 of 3 parts merged",
        processed: "412 ms",
        ignored: "2 paths ignored",
      }}
    />
  );
}

export function FromAPipelineOnGitLab() {
  return (
    <ProvenanceCard
      downloadUrl="/uploads/318/profile"
      provenance={{
        received_at: ago(19),
        profile_name: "lcov.info",
        profile_size: "1.2 MB",
        format: "lcov",
        ci_label: "GitLab CI #91744",
        ci_run_url: "https://gitlab.com/acme/billing/-/jobs/91744",
        uploader: "gocov/upload-component 1.1.0",
        uploader_kind: "component",
        part: "integration",
        parts_note: "1 of 2 parts merged",
        processed: "1.1 s",
        ignored: "",
      }}
    />
  );
}

export function AProfileWithLittleToSay() {
  return (
    <ProvenanceCard
      downloadUrl={null}
      provenance={{
        received_at: ago(96),
        profile_name: "coverage.xml",
        profile_size: "42 kB",
        format: "cobertura",
        ci_label: "",
        ci_run_url: "",
        uploader: "",
        uploader_kind: "",
        part: "",
        parts_note: "",
        processed: "",
        ignored: "",
      }}
    />
  );
}
