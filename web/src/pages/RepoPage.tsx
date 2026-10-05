import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link, useParams, useSearchParams } from "react-router";
import { Chip, LinkButton, Mono, Select } from "@/components/atoms";
import {
  Breadcrumbs,
  Card,
  KeyValue,
  KeyValueList,
  PageHeader,
  Pagination,
  QueryBoundary,
  SectionHeader,
  StatRow,
  StatTile,
} from "@/components/molecules";
import { FilesTable } from "@/components/organisms/FilesTable";
import { TrendChart } from "@/components/organisms/TrendChart";
import { UploadsTable } from "@/components/organisms/UploadsTable";
import { VerdictCard } from "@/components/organisms/VerdictCard";
import { repoQuery, repoUploadsQuery } from "@/lib/api/queries";
import type { RepoPage as RepoPageData } from "@/lib/api/types";
import { ciLabel, gateSummary, humanInt, pct, shortSha, timeAgo } from "@/lib/format";
import { usePageTitle } from "@/lib/title";
import { routes } from "@/lib/urls";
import "./RepoPage.css";

/**
 * A repository's standing: the latest verdict on the chosen branch, how its
 * coverage moved, the files behind it and the uploads that produced them.
 * The branch filter and the page number live in the URL.
 */
export default function RepoPage() {
  const params = useParams();
  const forge = params.forge ?? "";
  const slug = params["*"] ?? "";
  const [search, setSearch] = useSearchParams();
  const branch = search.get("branch") ?? "";
  const page = Math.max(0, Number(search.get("page") ?? 0) || 0);

  // Switching branch or page keeps the page it has: the new answer replaces
  // it when it lands, rather than flashing a skeleton in between. The
  // history pages on its own, so a page turn reads only the uploads.
  const query = useQuery({ ...repoQuery(forge, slug, branch), placeholderData: keepPreviousData });
  const history = useQuery({ ...repoUploadsQuery(forge, slug, branch, page), placeholderData: keepPreviousData });
  usePageTitle(`${slug} code coverage`);

  const go = (next: { branch?: string; page?: number }) => {
    const wanted = new URLSearchParams();
    const nextBranch = next.branch ?? branch;
    const nextPage = next.page ?? 0;
    if (nextBranch !== "") wanted.set("branch", nextBranch);
    if (nextPage > 0) wanted.set("page", String(nextPage));
    return wanted;
  };

  const pageLink = (p: number) => {
    const qs = go({ page: p }).toString();
    return routes.repo(forge, slug) + (qs === "" ? "" : "?" + qs);
  };

  return (
    <QueryBoundary query={query}>
      {(data) => (
        <div className="RepoPage stack stack-3">
          <PageHeader
            breadcrumbs={<Breadcrumbs items={[{ label: "Repositories", to: routes.dashboard() }, { label: <Mono>{slug}</Mono> }]} />}
            title={<Mono>{slug}</Mono>}
            meta={
              <>
                default branch <Mono>{data.repo.default_branch}</Mono>
                {data.summary !== null && <> &middot; {humanInt(data.summary.total_stmts)} statements</>}
              </>
            }
            actions={
              <>
                <Select
                  aria-label="Branch"
                  value={branch}
                  onChange={(e) => setSearch(go({ branch: e.target.value, page: 0 }))}
                >
                  <option value="">All branches</option>
                  {data.branches.map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </Select>
                {data.repo.can_settings && <LinkButton to={routes.repoSettings(forge, slug)}>Settings</LinkButton>}
              </>
            }
          />

          {data.summary !== null && <Summary data={data} summary={data.summary} />}

          {data.trend.length > 1 && (
            <section className="stack stack-1">
              <SectionHeader title="Coverage over time">
                <span className="muted small">
                  Total coverage on <Mono>{data.trend_branch}</Mono>
                </span>
              </SectionHeader>
              <Card>
                <Card.Body>
                  <TrendChart points={data.trend} branch={data.trend_branch} minCoverage={data.repo.gate.min_coverage} />
                </Card.Body>
              </Card>
              {data.repo.gate.min_coverage !== null && (
                <p className="muted small">
                  Red points failed the gate. The dashed line is the current minimum, {data.repo.gate.min_coverage}%.
                </p>
              )}
            </section>
          )}

          {data.files !== null && <FilesTable view={data.files} heading={`Files on ${data.trend_branch}`} />}

          <section className="stack stack-1">
            <SectionHeader title="Uploads">
              <span className="muted small">
                {branch === "" ? (
                  "All branches, newest first"
                ) : (
                  <>
                    On <Mono>{branch}</Mono>, newest first
                  </>
                )}
              </span>
            </SectionHeader>
            <QueryBoundary query={history}>
              {(uploads) => (
                <>
                  <UploadsTable
                    uploads={uploads.uploads}
                    empty={branch === "" ? "No uploads yet." : `No uploads on ${branch} yet.`}
                  />
                  {(page > 0 || uploads.has_older) && (
                    <Pagination
                      newer={page > 0 ? { to: pageLink(page - 1) } : { disabled: true }}
                      older={uploads.has_older ? { to: pageLink(page + 1) } : { disabled: true }}
                    />
                  )}
                </>
              )}
            </QueryBoundary>
          </section>
        </div>
      )}
    </QueryBoundary>
  );
}

/** The branch's current standing: the verdict, and the three figures under it. */
function Summary({ data, summary }: { data: RepoPageData; summary: NonNullable<RepoPageData["summary"]> }) {
  const { commit, verdict } = summary;
  const gate = gateSummary(data.repo.gate);
  return (
    <div className="stack">
      <VerdictCard
        verdict={verdict}
        meta={
          <KeyValueList>
            <KeyValue
              label="Commit"
              value={
                <>
                  <Link to={routes.upload(commit.upload_id)}>
                    <Mono>{shortSha(commit.sha)}</Mono>
                  </Link>{" "}
                  <span className="muted small">&middot; {timeAgo(commit.at)}</span>
                </>
              }
            />
            <KeyValue
              label="Branch"
              value={
                <span className="row">
                  <Mono>{commit.branch}</Mono>
                  {commit.pr_id !== "" ? (
                    <Chip tone="accent">PR #{commit.pr_id}</Chip>
                  ) : (
                    commit.is_default && <Chip tone="plain">default</Chip>
                  )}
                </span>
              }
            />
            <KeyValue
              label="Compared to"
              value={
                verdict.base === null ? (
                  <span className="muted">&mdash;</span>
                ) : (
                  <>
                    <Link to={routes.upload(verdict.base.upload_id)}>
                      <Mono>{shortSha(verdict.base.sha)}</Mono>
                    </Link>{" "}
                    <span className="muted small">&middot; {pct(verdict.base.coverage)}</span>
                  </>
                )
              }
            />
          </KeyValueList>
        }
      />
      <StatRow>
        <StatTile
          label="Uncovered statements"
          value={humanInt(summary.total_stmts - summary.covered_stmts)}
          hint={`of ${humanInt(summary.total_stmts)}`}
        />
        <StatTile
          label="Gate"
          value={
            gate === "" ? (
              <span className="muted small">not configured</span>
            ) : (
              <Mono className="small">{gate}</Mono>
            )
          }
        />
        <StatTile
          label="Last upload"
          value={
            summary.last_upload === null ? (
              <span className="muted">&mdash;</span>
            ) : (
              <span className="RepoPage__stat">{timeAgo(summary.last_upload.at)}</span>
            )
          }
          hint={ciLabel(summary.last_upload?.ci_provider ?? "") || undefined}
        />
      </StatRow>
    </div>
  );
}
