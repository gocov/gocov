import { CoverageBar, KeyValue, KeyValueList, Mono, QueryBoundary } from "gocov-web";

interface Report {
  repo: string;
  branch: string;
  commit: string;
  coverage: number;
}

// The boundary only reads isPending / isError / error / data off the query, so a
// settled TanStack result can be stood up by hand for each of its three states.
const settled = {
  repo: "acme/api",
  branch: "main",
  commit: "a1b2c3d",
  coverage: 74,
};

function report(data: Report) {
  return (
    <KeyValueList>
      <KeyValue label="Repository" value={<Mono>{data.repo}</Mono>} />
      <KeyValue label="Branch" value={<Mono>{data.branch}</Mono>} />
      <KeyValue label="Commit" value={<Mono>{data.commit}</Mono>} />
      <KeyValue label="Coverage" value={<CoverageBar value={data.coverage} />} />
    </KeyValueList>
  );
}

export function Loaded() {
  const query = { isPending: false, isError: false, data: settled } as never;
  return <QueryBoundary query={query}>{(data: Report) => report(data)}</QueryBoundary>;
}

export function Pending() {
  const query = { isPending: true, isError: false } as never;
  return <QueryBoundary query={query}>{(data: Report) => report(data)}</QueryBoundary>;
}

export function Failed() {
  const query = {
    isPending: false,
    isError: true,
    error: new Error("The server did not answer in time."),
    refetch: () => Promise.resolve(),
  } as never;
  return <QueryBoundary query={query}>{(data: Report) => report(data)}</QueryBoundary>;
}
