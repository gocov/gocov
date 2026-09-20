import { Button, Card, CoverageBar, Delta, Mono, SectionHeader } from "gocov-web";

export function WithANote() {
  return <SectionHeader title="Files">28 files · 4 changed</SectionHeader>;
}

export function WithAControl() {
  return (
    <SectionHeader title="Uploads">
      <Button size="sm">All branches</Button>
    </SectionHeader>
  );
}

export function AboveATable() {
  return (
    <div className="stack">
      <SectionHeader title="Repositories">12 repositories · 1 stale</SectionHeader>
      <Card>
        <Card.Body flush>
          <table>
            <thead>
              <tr>
                <th>Repository</th>
                <th>Branch</th>
                <th>Coverage</th>
                <th>Δ</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>
                  <Mono>acme/api</Mono>
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
                  <Mono>acme/worker</Mono>
                </td>
                <td>
                  <Mono>main</Mono>
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
    </div>
  );
}
