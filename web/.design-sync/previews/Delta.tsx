import { Card, Delta, Mono } from "gocov-web";

export function Directions() {
  return (
    <div className="row row-2">
      <Delta value={2.4} />
      <Delta value={-1.1} />
      <Delta value={12.8} />
      <Delta value={-6.3} />
    </div>
  );
}

export function NothingToReport() {
  return (
    <div className="row row-2">
      <span className="row">
        <Delta value={0.02} /> <span className="muted small">rounds to no change</span>
      </span>
      <span className="row">
        <Delta value={null} /> <span className="muted small">no baseline on this branch</span>
      </span>
    </div>
  );
}

export function AgainstTheBaseBranch() {
  return (
    <Card>
      <Card.Header title="Recent uploads" />
      <Card.Body flush>
        <table>
          <thead>
            <tr>
              <th>Commit</th>
              <th>Branch</th>
              <th>Δ</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>
                <Mono>a1b2c3d</Mono>
              </td>
              <td>
                <Mono>main</Mono>
              </td>
              <td>
                <Delta value={2.8} />
              </td>
            </tr>
            <tr>
              <td>
                <Mono>9f2c41d</Mono>
              </td>
              <td>
                <Mono>fix/upload</Mono>
              </td>
              <td>
                <Delta value={-1.1} />
              </td>
            </tr>
            <tr>
              <td>
                <Mono>4d81ea0</Mono>
              </td>
              <td>
                <Mono>chore/deps</Mono>
              </td>
              <td>
                <Delta value={null} />
              </td>
            </tr>
          </tbody>
        </table>
      </Card.Body>
    </Card>
  );
}
