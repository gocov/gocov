import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { CoverageFigure, Delta, Mono } from "@/components/atoms";
import { Breadcrumbs, Card, PageHeader, QueryBoundary, UncoveredRanges } from "@/components/molecules";
import { SourceViewer } from "@/components/organisms/SourceViewer";
import { sourceQuery } from "@/lib/api/queries";
import type { SourcePage as SourceData } from "@/lib/api/types";
import { humanInt, plural, shortSha, splitPath } from "@/lib/format";
import { usePageTitle } from "@/lib/title";
import { routes } from "@/lib/urls";
import "./SourcePage.css";

/** One file at one commit: the header, then the source or why there is none. */
export default function SourcePage() {
  const params = useParams();
  const path = params["*"] ?? "";
  const query = useQuery(sourceQuery(params.id ?? "", path));
  usePageTitle(path);
  return <QueryBoundary query={query}>{(data) => <Source data={data} />}</QueryBoundary>;
}

const Sep = () => (
  <span className="SourcePage__sep" aria-hidden="true">
    &middot;
  </span>
);

function Source({ data }: { data: SourceData }) {
  const { repo, upload, file } = data;
  const [dir, base] = splitPath(file.path);
  const newly = data.lines.filter((line) => line.new_miss).length;

  return (
    <div className="stack stack-3">
      <PageHeader
        breadcrumbs={
          <Breadcrumbs
            items={[
              { label: "Repositories", to: routes.dashboard() },
              { label: <Mono>{repo.slug}</Mono>, to: routes.repo(repo.forge, repo.slug) },
              { label: <Mono>{shortSha(upload.sha)}</Mono>, to: routes.upload(upload.id) },
              { label: <Mono>{file.path}</Mono> },
            ]}
          />
        }
        title={
          <Mono>
            {dir !== "" && <span className="SourcePage__dir">{dir}</span>}
            {base}
          </Mono>
        }
        meta={
          <span className="row">
            <span>
              {humanInt(file.covered_stmts)} of {humanInt(file.total_stmts)} statements covered
            </span>
            {data.delta !== null && (
              <>
                <Sep />
                <Delta value={data.delta} />
              </>
            )}
            <Sep />
            <span>
              commit{" "}
              <Link to={routes.upload(upload.id)}>
                <Mono>{shortSha(upload.sha)}</Mono>
              </Link>
            </span>
            {newly > 0 && (
              <>
                <Sep />
                <span className="SourcePage__newmiss">{plural(newly, "line")} newly uncovered</span>
              </>
            )}
          </span>
        }
        actions={
          <span className="SourcePage__figure">
            <span className="SourcePage__figureLabel">File coverage</span>
            <CoverageFigure value={file.coverage} size="md" />
          </span>
        }
      />

      {data.unavailable !== "" ? (
        <Card>
          <Card.Body>
            <div className="stack stack-1">
              <p>
                <strong>Source is unavailable:</strong> <span className="muted">{data.unavailable}</span>
              </p>
              {data.uncovered !== "" ? (
                <p className="row">
                  Uncovered lines: <UncoveredRanges ranges={data.uncovered} />
                </p>
              ) : (
                <p className="muted small">Every statement in this file is covered.</p>
              )}
            </div>
          </Card.Body>
        </Card>
      ) : (
        <SourceViewer lines={data.lines} newlyUncovered={newly} />
      )}
    </div>
  );
}
