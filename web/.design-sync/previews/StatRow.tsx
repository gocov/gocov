import { Chip, CoverageFigure, StatRow, StatTile } from "gocov-web";

export function FourTiles() {
  return (
    <StatRow>
      <StatTile label="Coverage" value={<CoverageFigure value={74} size="md" />} hint="statement-weighted" />
      <StatTile label="Gates passing" value="6/8" />
      <StatTile label="Stale" value="1" hint="no upload in 14 days" />
      <StatTile label="Reporting" value={<Chip tone="good">Connected</Chip>} hint="gocov[bot]" />
    </StatRow>
  );
}

export function ThreeTiles() {
  return (
    <StatRow>
      <StatTile label="Repositories" value="12" hint="in acme" />
      <StatTile label="Uploads today" value="37" />
      <StatTile label="Diff coverage" value={<CoverageFigure value={91.4} size="md" />} hint="acme/api #418" />
    </StatRow>
  );
}

export function TwoTiles() {
  return (
    <StatRow>
      <StatTile label="Statements" value="12,481" />
      <StatTile label="Covered" value="9,236" />
    </StatRow>
  );
}
