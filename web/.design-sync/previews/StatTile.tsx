import { Chip, CoverageFigure, Delta, StatRow, StatTile } from "gocov-web";

export function PlainValues() {
  return (
    <StatRow>
      <StatTile label="Statements" value="12,481" />
      <StatTile label="Covered" value="9,236" />
      <StatTile label="Gates passing" value="6/8" />
    </StatRow>
  );
}

export function ElementValues() {
  return (
    <StatRow>
      <StatTile label="Coverage" value={<CoverageFigure value={74} size="md" />} />
      <StatTile label="Change" value={<Delta value={2.8} />} />
      <StatTile label="Reporting" value={<Chip tone="good">Connected</Chip>} />
    </StatRow>
  );
}

export function WithHints() {
  return (
    <StatRow>
      <StatTile label="Coverage" value={<CoverageFigure value={74} size="md" />} hint="statement-weighted" />
      <StatTile label="Stale" value="1" hint="no upload in 14 days" />
      <StatTile label="Last upload" value="3 hours ago" hint="a1b2c3d on main" />
    </StatRow>
  );
}
