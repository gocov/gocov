import { Card, CoverageBar, Mono } from "gocov-web";

export function Thresholds() {
  return (
    <div className="stack stack-1">
      <CoverageBar value={84.2} />
      <CoverageBar value={63} />
      <CoverageBar value={41.4} />
    </div>
  );
}

export function EndsOfTheScale() {
  return (
    <div className="stack stack-1">
      <CoverageBar value={100} />
      <CoverageBar value={75.1} />
      <CoverageBar value={50} />
      <CoverageBar value={0} />
    </div>
  );
}

export function AcrossRepositories() {
  return (
    <Card>
      <Card.Header title="Repositories" />
      <Card.Body flush>
        <table>
          <thead>
            <tr>
              <th>Repository</th>
              <th>Coverage</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>
                <Mono>acme/api</Mono>
              </td>
              <td>
                <CoverageBar value={74} />
              </td>
            </tr>
            <tr>
              <td>
                <Mono>acme/web</Mono>
              </td>
              <td>
                <CoverageBar value={88.6} />
              </td>
            </tr>
            <tr>
              <td>
                <Mono>acme/billing</Mono>
              </td>
              <td>
                <CoverageBar value={41.4} />
              </td>
            </tr>
          </tbody>
        </table>
      </Card.Body>
    </Card>
  );
}
