import { Card, Mono, SectionHeader, TrendChart } from "gocov-web";

// The chart prints the first and last date, so the series is measured from
// Date.now(): the grading sheet pins the clock, the live card does not, and a
// literal would read as history in one of them.
const daysAgo = (days: number) => new Date(Date.now() - days * 86_400_000).toISOString();

const shas = [
  "a1b2c3d4e5f67890",
  "9f2c41d8a7b30000",
  "7c1de90fa2b41188",
  "3ab77cc10de92244",
  "5e0b9912aa47c630",
  "c04a7b18ee520931",
  "1d93ff20b7a6c455",
  "8b41c07de9f31200",
  "60ea5b3c9d18f722",
  "e27c9104ab5d3860",
  "44f8b0d7c21ae935",
  "b915e6c3708d24af",
  "2fd0ac845b617390",
];

/** `coverage` oldest first; `failed` marks the uploads the gate turned down. */
function series(coverage: number[], failed: number[] = []) {
  const last = coverage.length - 1;
  return coverage.map((value, i) => ({
    upload_id: 400 + i,
    sha: shas[i % shas.length] as string,
    coverage: value,
    at: daysAgo((last - i) * 3),
    gate_failed: failed.includes(i),
  }));
}

export function RisingOnMain() {
  return (
    <section className="stack stack-1">
      <SectionHeader title="Coverage over time">
        <span className="muted small">
          Total coverage on <Mono>main</Mono>
        </span>
      </SectionHeader>
      <Card>
        <Card.Body>
          <TrendChart
            points={series([61.2, 64.8, 63.1, 70.5, 73.9, 76.2, 79.8, 78.4, 82.3], [0, 1, 2])}
            branch="main"
            minCoverage={75}
          />
        </Card.Body>
      </Card>
      <p className="muted small">Red points failed the gate. The dashed line is the current minimum, 75%.</p>
    </section>
  );
}

export function TwoUploads() {
  return (
    <section className="stack stack-1">
      <SectionHeader title="Coverage over time">
        <span className="muted small">
          Total coverage on <Mono>main</Mono>
        </span>
      </SectionHeader>
      <Card>
        <Card.Body>
          <TrendChart points={series([68.4, 74.0])} branch="main" minCoverage={null} />
        </Card.Body>
      </Card>
      <p className="muted small">Two uploads is the shortest trend gocov will draw.</p>
    </section>
  );
}

export function LabelClearsThePeak() {
  // Thirteen points sit close enough together that the current-value label
  // reaches back over its neighbour, and the gate at 70 pushes the peak to
  // the top of the canvas — so the label drops below the pair instead.
  return (
    <section className="stack stack-1">
      <SectionHeader title="Coverage over time">
        <span className="muted small">
          Total coverage on <Mono>main</Mono>
        </span>
      </SectionHeader>
      <Card>
        <Card.Body>
          <TrendChart
            points={series([88.0, 89.2, 88.6, 90.4, 89.9, 91.1, 90.2, 91.8, 90.6, 91.4, 89.7, 92.4, 90.1])}
            branch="main"
            minCoverage={70}
          />
        </Card.Body>
      </Card>
      <p className="muted small">The dashed line is the current minimum, 70%.</p>
    </section>
  );
}

export function NoGateConfigured() {
  return (
    <section className="stack stack-1">
      <SectionHeader title="Coverage over time">
        <span className="muted small">
          Total coverage on <Mono>release/2.4</Mono>
        </span>
      </SectionHeader>
      <Card>
        <Card.Body>
          <TrendChart
            points={series([78.0, 77.2, 76.8, 75.1, 74.6, 72.9, 71.4])}
            branch="release/2.4"
            minCoverage={null}
          />
        </Card.Body>
      </Card>
      <p className="muted small">No minimum is set on this repository, so nothing can fail.</p>
    </section>
  );
}
