import type { ReactNode } from "react";
import { Link } from "react-router";
import { Chip, CoverageBar, Mono } from "@/components/atoms";
import { Card, EmptyState } from "@/components/molecules";
import type { UploadRow } from "@/lib/api/types";
import { shortSha, timeAgo } from "@/lib/format";
import { routes } from "@/lib/urls";
import "./UploadsTable.css";

const gateChip: Record<UploadRow["gate"], ReactNode> = {
  pass: <Chip tone="good">Passed</Chip>,
  fail: <Chip tone="bad">Failed</Chip>,
  none: <Chip>No gate</Chip>,
};

/** A repository's upload history, newest first. */
export function UploadsTable({ uploads, empty = "No uploads yet." }: { uploads: UploadRow[]; empty?: ReactNode }) {
  return (
    <Card>
      {uploads.length === 0 ? (
        <Card.Body>
          <EmptyState message={empty} />
        </Card.Body>
      ) : (
        <Card.Body flush>
          <table>
            <thead>
              <tr>
                <th>Commit</th>
                <th>Coverage</th>
                <th>Gate</th>
                <th className="hide-sm">Uploaded</th>
              </tr>
            </thead>
            <tbody>
              {uploads.map((upload) => (
                <tr key={upload.id}>
                  <td>
                    <Link to={routes.upload(upload.id)}>
                      <Mono>{shortSha(upload.sha)}</Mono>
                    </Link>
                    <span className="UploadsTable__branch">
                      <Mono>{upload.branch}</Mono>
                      {upload.pr_id !== "" && <Chip tone="accent">PR #{upload.pr_id}</Chip>}
                    </span>
                  </td>
                  <td>
                    <CoverageBar value={upload.coverage} />
                  </td>
                  <td>{gateChip[upload.gate]}</td>
                  <td className="muted small hide-sm">{timeAgo(upload.at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card.Body>
      )}
    </Card>
  );
}
