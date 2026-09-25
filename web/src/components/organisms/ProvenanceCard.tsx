import type { ReactNode } from "react";
import { Chip, LinkButton, Mono } from "@/components/atoms";
import { Card, KeyValue, KeyValueList } from "@/components/molecules";
import type { Provenance } from "@/lib/api/types";
import { ciLabel, duration, humanBytes, plural, timeAgo, uploaderKindLabel } from "@/lib/format";
import "./ProvenanceCard.css";

/** "5 Sep 2026, 14:02" — the full moment, beside the relative one. */
const received = (iso: string) => {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  return at.toLocaleString("en-GB", { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });
};

/** How an upload arrived: the profile, the CI run behind it, and who sent it. */
export function ProvenanceCard({ provenance, downloadUrl }: { provenance: Provenance; downloadUrl: string | null }) {
  const p = provenance;
  const rows: { label: string; value: ReactNode }[] = [];
  const ci = ciLabel(p.ci_provider);
  const kind = uploaderKindLabel(p.uploader_kind);
  const partsNote = p.parts > 1 ? `merged from ${p.parts} parts` : "single profile, no merge";
  const ignored = p.ignored_files > 0 ? `${plural(p.ignored_files, "file")} ignored` : "";

  if (p.received_at !== "") {
    rows.push({
      label: "Received",
      value: (
        <>
          {received(p.received_at)} <span className="muted small">&middot; {timeAgo(p.received_at)}</span>
        </>
      ),
    });
  }
  if (p.profile_name !== "") {
    rows.push({
      label: "Profile",
      value: (
        <>
          <Mono>{p.profile_name}</Mono> {p.format !== "" && <Chip tone="plain">{p.format}</Chip>}
          {p.profile_bytes > 0 && <span className="muted small"> &middot; {humanBytes(p.profile_bytes)}</span>}
        </>
      ),
    });
  }
  if (ci !== "") {
    rows.push({
      label: "CI run",
      value: (
        <>
          {ci}
          {p.ci_run_url !== "" && (
            <>
              {" "}
              &middot;{" "}
              <a href={p.ci_run_url} rel="nofollow noopener" target="_blank">
                view run
              </a>
            </>
          )}
        </>
      ),
    });
  }
  if (p.uploader !== "") {
    rows.push({
      label: "Uploader",
      value: (
        <>
          <Mono>{p.uploader}</Mono>
          {kind !== "" && <span className="muted small"> &middot; {kind}</span>}
        </>
      ),
    });
  }
  // Every upload has something to say here: how the commit's parts merged.
  rows.push({
    label: "Flags",
    value: (
      <>
        {p.part !== "" && <Mono>{p.part}</Mono>}
        <span className="muted small">
          {p.part !== "" && <> &middot; </>}
          {partsNote}
          {ignored !== "" && <> &middot; {ignored}</>}
        </span>
      </>
    ),
  });
  if (p.process_ms > 0) rows.push({ label: "Processed in", value: duration(p.process_ms) });

  const half = Math.ceil(rows.length / 2);
  const columns = [rows.slice(0, half), rows.slice(half)];

  return (
    <Card>
      <Card.Header
        title="Upload"
        actions={
          downloadUrl === null ? undefined : (
            <LinkButton href={downloadUrl} size="sm" icon="external">
              Download profile
            </LinkButton>
          )
        }
      />
      <Card.Body>
        <div className="ProvenanceCard">
          {columns.map((column, i) => (
            <KeyValueList key={i}>
              {column.map((row) => (
                <KeyValue key={row.label} label={row.label} value={row.value} />
              ))}
            </KeyValueList>
          ))}
        </div>
      </Card.Body>
    </Card>
  );
}
