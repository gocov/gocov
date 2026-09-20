import { Card, NotFoundState, PageHeader } from "gocov-web";

export function Panel() {
  return <NotFoundState />;
}

export function InsideACard() {
  return (
    <Card>
      <Card.Header title="acme/api" />
      <Card.Body>
        <NotFoundState />
      </Card.Body>
    </Card>
  );
}

export function UnderAPageHeader() {
  return (
    <div className="stack">
      <PageHeader title="Repository" />
      <NotFoundState />
    </div>
  );
}
