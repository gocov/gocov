import { render, screen } from "@testing-library/react";
import type { Provenance } from "@/lib/api/types";
import { ProvenanceCard } from "./ProvenanceCard";

const provenance = (over: Partial<Provenance> = {}): Provenance => ({
  received_at: new Date().toISOString(),
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
  ...over,
});

test("the card lists how the upload arrived", () => {
  render(<ProvenanceCard provenance={provenance()} downloadUrl="/uploads/412/profile" />);
  expect(screen.getByRole("heading", { name: "Upload" })).toBeInTheDocument();
  expect(screen.getByText("coverage.out")).toBeInTheDocument();
  expect(screen.getByText("go")).toBeInTheDocument();
  expect(screen.getByText(/184 kB/)).toBeInTheDocument();
  expect(screen.getByText("gocov-action v1.17.0")).toBeInTheDocument();
  expect(screen.getByText(/3 of 3 parts merged/)).toBeInTheDocument();
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
      provenance={provenance({ ci_label: "", ci_run_url: "", uploader: "", processed: "", part: "", ignored: "" })}
      downloadUrl={null}
    />,
  );
  expect(screen.queryByText("CI run")).not.toBeInTheDocument();
  expect(screen.queryByText("Uploader")).not.toBeInTheDocument();
  expect(screen.queryByText("Processed in")).not.toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Download profile" })).not.toBeInTheDocument();
  // The parts note alone still earns the Flags row.
  expect(screen.getByText("Flags")).toBeInTheDocument();
});
