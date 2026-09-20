import { CoverageFigure } from "gocov-web";

export function Thresholds() {
  return (
    <div className="row row-2">
      <CoverageFigure value={84.2} />
      <CoverageFigure value={62.5} />
      <CoverageFigure value={38} />
    </div>
  );
}

export function Sizes() {
  return (
    <div className="row row-2">
      <CoverageFigure value={74} />
      <CoverageFigure value={74} size="md" />
    </div>
  );
}

export function NoReportYet() {
  return (
    <div className="row row-2">
      <CoverageFigure value={null} />
      <span className="muted small">No upload on main yet.</span>
    </div>
  );
}

export function AsAHeadline() {
  return (
    <div className="stack stack-1">
      <CoverageFigure value={74} />
      <span className="muted small">
        acme/api · main · a1b2c3d · statement-weighted across 28 files
      </span>
    </div>
  );
}
