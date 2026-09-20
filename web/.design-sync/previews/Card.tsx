import { Button, Card, Chip, CoverageBar, Delta, Mono } from "gocov-web";

export function HeaderBodyFooter() {
  return (
    <Card>
      <Card.Header title="Uploads" actions={<Chip tone="plain">Owner</Chip>} />
      <Card.Body>Every repository under acme/ uploads with this token.</Card.Body>
      <Card.Footer>
        <Button>Rotate token</Button>
        <span>The old token stops working the moment you rotate.</span>
      </Card.Footer>
    </Card>
  );
}

export function FlushTable() {
  return (
    <Card>
      <Card.Header title="Recent uploads" />
      <Card.Body flush>
        <table>
          <thead>
            <tr>
              <th>Commit</th>
              <th>Branch</th>
              <th>Coverage</th>
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
                <CoverageBar value={74} />
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
                <CoverageBar value={62.5} />
              </td>
              <td>
                <Delta value={-1.1} />
              </td>
            </tr>
          </tbody>
        </table>
      </Card.Body>
    </Card>
  );
}

export function Danger() {
  return (
    <Card danger>
      <Card.Header title="Delete workspace" />
      <Card.Body>Removes acme, its 12 repositories and every coverage report gocov holds for them.</Card.Body>
      <Card.Footer>
        <Button variant="danger">Delete this workspace</Button>
        <span>This cannot be undone.</span>
      </Card.Footer>
    </Card>
  );
}
