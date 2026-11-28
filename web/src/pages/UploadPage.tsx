import { useQuery } from "@tanstack/react-query";
import { Link, useParams, useSearchParams } from "react-router";
import { Chip, Mono, Tooltip } from "@/components/atoms";
import {
  Breadcrumbs,
  KeyValue,
  KeyValueList,
  PageHeader,
  QueryBoundary,
  StatRow,
  StatTile,
} from "@/components/molecules";
import { FilesTable } from "@/components/organisms/FilesTable";
import { ProvenanceCard } from "@/components/organisms/ProvenanceCard";
import { VerdictCard } from "@/components/organisms/VerdictCard";
import { uploadQuery } from "@/lib/api/queries";
import type { CarriedPart, UploadPage as UploadPageData } from "@/lib/api/types";
import { humanInt, pct, plural, shortSha, timeAgo } from "@/lib/format";
import { usePageTitle } from "@/lib/title";
import { routes } from "@/lib/urls";

const tokenlessHint =
  "Uploaded from a fork's CI without a token; authenticated by verifying the workflow run, not a repository secret.";

/** One upload: its verdict, the diff it covered, its files and where it came from. */
export default function UploadPage() {
  const id = useParams().id ?? "";
  const [search] = useSearchParams();
  const query = useQuery(uploadQuery(id, search.get("parts") ?? ""));
  usePageTitle(query.data && `${query.data.repo.slug} @ ${shortSha(query.data.upload.sha)}`);

  return (
    <QueryBoundary query={query}>
      {(data) => (
        <div className="UploadPage stack stack-3">
          <Header data={data} />
          <PartsNote data={data} />

          <VerdictCard
            verdict={data.verdict}
            meta={
              <KeyValueList>
                <KeyValue
                  label="Statements"
                  value={
                    <>
                      {humanInt(data.covered_stmts)}/{humanInt(data.total_stmts)}{" "}
                      <span className="muted small">covered</span>
                    </>
                  }
                />
                <KeyValue
                  label="Base"
                  value={
                    data.verdict.base === null ? (
                      <span className="muted">&mdash;</span>
                    ) : (
                      <>
                        <Link to={routes.upload(data.verdict.base.upload_id, data.files.merged)}>
                          <Mono>{shortSha(data.verdict.base.sha)}</Mono>
                        </Link>{" "}
                        <span className="muted small">&middot; {pct(data.verdict.base.coverage)}</span>
                      </>
                    )
                  }
                />
                <KeyValue
                  label="Report"
                  value={
                    <>
                      {plural(data.files.files.length, "file")}{" "}
                      <span className="muted small">&middot; {data.provenance.format} profile</span>
                    </>
                  }
                />
              </KeyValueList>
            }
          />

          {data.diff !== null && data.diff.total_lines > 0 && (
            <StatRow>
              <StatTile
                label="Diff coverage"
                value={pct(data.diff.coverage)}
                hint={`${humanInt(data.diff.covered_lines)} of ${humanInt(data.diff.total_lines)} new lines`}
              />
              <StatTile
                label="Uncovered lines added"
                value={humanInt(data.diff.total_lines - data.diff.covered_lines)}
              />
              <StatTile
                label="Changed files"
                value={humanInt(data.diff.changed_files)}
                hint={data.diff.unmatched_files > 0 ? `+${data.diff.unmatched_files} without data` : undefined}
              />
            </StatRow>
          )}

          <FilesTable view={data.files} />

          <div className="stack stack-1">
            <ProvenanceCard provenance={data.provenance} downloadUrl={data.download_url} />
            {data.verdict.base !== null && (
              <p className="muted small">
                Compared against baseline{" "}
                <Link to={routes.upload(data.verdict.base.upload_id, data.files.merged)}>
                  <Mono>{shortSha(data.verdict.base.sha)}</Mono>
                </Link>{" "}
                ({pct(data.verdict.base.coverage)}),{" "}
                {data.files.merged
                  ? "the last gate-passing commit before this one, every part merged."
                  : "the last gate-passing upload on this branch."}
              </p>
            )}
          </div>
        </div>
      )}
    </QueryBoundary>
  );
}

/**
 * For a commit uploaded in parts, which of its two views this is — the
 * upload alone, or every part merged (what the PR comment and the build
 * status state) — with the way to the other, and the parts the commit
 * took from earlier commits.
 */
function PartsNote({ data }: { data: UploadPageData }) {
  const { parts, carried } = data.provenance;
  if (parts < 2) return null;
  const merged = data.files.merged;
  return (
    <p className="muted small">
      {merged
        ? `Every part of this commit merged, ${parts} in all.`
        : `This upload is one of the ${parts} parts of its commit.`}
      <CarriedSentence carried={carried} />{" "}
      <Link to={routes.upload(data.upload.id, !merged)}>{merged ? "This upload alone" : "Every part merged"}</Link>
    </p>
  );
}

/** " Carried forward: api from 1a2b3c4d5e6f." — each commit linking to the upload that measured the part. */
function CarriedSentence({ carried }: { carried: CarriedPart[] }) {
  if (carried.length === 0) return null;
  return (
    <>
      {" "}
      Carried forward:{" "}
      {carried.map((c, i) => (
        <span key={c.part}>
          {i > 0 && ", "}
          <Mono>{c.part}</Mono> from{" "}
          <Link to={routes.upload(c.upload_id)}>
            <Mono>{shortSha(c.commit)}</Mono>
          </Link>
        </span>
      ))}
      .
    </>
  );
}

function Header({ data }: { data: UploadPageData }) {
  const { upload, repo } = data;
  const sha = shortSha(upload.sha);
  return (
    <PageHeader
      breadcrumbs={
        <Breadcrumbs
          items={[
            { label: "Repositories", to: routes.dashboard() },
            { label: <Mono>{repo.slug}</Mono>, to: routes.repo(repo.forge, repo.slug) },
            { label: <Mono>{sha}</Mono> },
          ]}
        />
      }
      title={upload.commit_message === "" ? <Mono>{sha}</Mono> : upload.commit_message}
      meta={
        <span className="row">
          <Mono>{sha}</Mono>
          <span>
            on <Mono>{upload.branch}</Mono>
          </span>
          {upload.pr_id !== "" && <Chip tone="accent">PR #{upload.pr_id}</Chip>}
          {upload.tokenless && (
            <Tooltip text={tokenlessHint}>
              <Chip tone="warn">unverified contributor upload</Chip>
            </Tooltip>
          )}
          {upload.commit_author !== "" && (
            <span>
              by <Mono>{upload.commit_author}</Mono>
            </span>
          )}
          <span>&middot; {timeAgo(upload.at)}</span>
        </span>
      }
    />
  );
}
