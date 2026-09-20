import { Card, Mono, UncoveredRanges } from "gocov-web";

export function AShortList() {
  return <UncoveredRanges ranges="12-18, 40, 55-57" />;
}

export function CutShort() {
  return <UncoveredRanges ranges="12-18, 40, 55-57, 61, 70-74, 88, 91, 104-110, 119, 130" max={6} />;
}

export function NothingUncovered() {
  return (
    <span className="muted">
      Uncovered lines: <UncoveredRanges ranges="" />
    </span>
  );
}

export function InAFilesTable() {
  return (
    <Card>
      <Card.Body flush>
        <table>
          <thead>
            <tr>
              <th>File</th>
              <th>Newly uncovered</th>
              <th>Uncovered</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>
                <Mono>internal/server/upload.go</Mono>
              </td>
              <td>
                <UncoveredRanges ranges="88, 91" />
              </td>
              <td>
                <UncoveredRanges ranges="12-18, 40, 55-57, 61, 88, 91" max={4} />
              </td>
            </tr>
            <tr>
              <td>
                <Mono>internal/core/report.go</Mono>
              </td>
              <td>
                <UncoveredRanges ranges="" />
              </td>
              <td>
                <UncoveredRanges ranges="104-110, 119" />
              </td>
            </tr>
          </tbody>
        </table>
      </Card.Body>
    </Card>
  );
}
