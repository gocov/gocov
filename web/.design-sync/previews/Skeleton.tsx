import { Card, Skeleton } from "gocov-web";

export function WhileTheQueryIsPending() {
  return <Skeleton />;
}

export function TwoLines() {
  return <Skeleton lines={2} />;
}

export function InsideACard() {
  return (
    <Card>
      <Card.Header title="Recent uploads" />
      <Card.Body>
        <Skeleton lines={5} />
      </Card.Body>
    </Card>
  );
}
