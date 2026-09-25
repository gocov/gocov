import { render, screen } from "@testing-library/react";
import type { Provenance } from "@/lib/api/types";
import { ProvenanceCard } from "./ProvenanceCard";

const provenance = (over: Partial<Provenance> = {}): Provenance => ({
  received_at: new Date().toISOString(),
  profile_name: "coverage.out",
  profile_bytes: 188_416,
  format: "go",
  ci_provider: "github",
  ci_run_url: "https://github.com/acme/api/actions/runs/2184",
  uploader: "gocov-action v1.17.0",
  uploader_kind: "action",
  part: "unit",
  parts: 3,
  process_ms: 412,
  ignored_files: 2,
  ...over,
});

test("the card lists how the upload arrived", () => {
  render(<ProvenanceCard provenance={provenance()} downloadUrl="/uploads/412/profile" />);
  expect(screen.getByRole("heading", { name: "Upload" })).toBeInTheDocument();
  expect(screen.getByText("coverage.out")).toBeInTheDocument();
  expect(screen.getByText("go")).toBeInTheDocument();
  expect(screen.getByText(/184 KB/)).toBeInTheDocument();
  expect(screen.getByText(/GitHub Actions/)).toBeInTheDocument();
  expect(screen.getByText("gocov-action v1.17.0")).toBeInTheDocument();
  expect(screen.getByText(/· Action/)).toBeInTheDocument();
  expect(screen.getByText(/merged from 3 parts · 2 files ignored/)).toBeInTheDocument();
  expect(screen.getByText("412 ms")).toBeInTheDocument();
  expect(screen.getByText(/just now/)).toBeInTheDocument();
});

test("the CI run and the profile download are links out of the app", () => {
  render(<ProvenanceCard provenance={provenance()} downloadUrl="/uploads/412/profile" />);
  expect(screen.getByRole("link", { name: "view run" })).toHaveAttribute(
    "href",
    "https://github.com/acme/api/actions/runs/2184",
  );
  expect(screen.getByRole("link", { name: "Download profile" })).toHaveAttribute("href", "/uploads/412/profile");
});

test("rows with nothing to say, and a profile that cannot be downloaded, are left out", () => {
  render(
    <ProvenanceCard
      provenance={provenance({
        profile_bytes: 0,
        ci_provider: "",
        ci_run_url: "",
        uploader: "",
        process_ms: 0,
        part: "",
        parts: 1,
        ignored_files: 0,
      })}
      downloadUrl={null}
    />,
  );
  expect(screen.queryByText("CI run")).not.toBeInTheDocument();
  expect(screen.queryByText("Uploader")).not.toBeInTheDocument();
  expect(screen.queryByText("Processed in")).not.toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Download profile" })).not.toBeInTheDocument();
  // The parts note alone still earns the Flags row.
  expect(screen.getByText("Flags")).toBeInTheDocument();
  expect(screen.getByText("single profile, no merge")).toBeInTheDocument();
  expect(screen.queryByText(/ B$|KB|MB/)).not.toBeInTheDocument();
});
