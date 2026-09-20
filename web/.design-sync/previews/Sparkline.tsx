import { Mono, Sparkline } from "gocov-web";

export function InARepoRow() {
  return (
    <span className="row row-2">
      <Mono>acme/api</Mono>
      <Sparkline series={[61, 63, 62, 66, 69, 74]} label="Coverage trend for acme/api: rising" />
      <span className="num">74.0%</span>
    </span>
  );
}

export function Rising() {
  return <Sparkline series={[61, 63, 62, 66, 69, 74]} />;
}

export function Falling() {
  return <Sparkline series={[80, 79, 76, 71, 70, 66]} />;
}

export function Flat() {
  return <Sparkline series={[70, 70.3, 69.8, 70.1, 70]} />;
}

export function Stale() {
  return <Sparkline series={[74, 72, 73, 70]} stale />;
}

export function TooShort() {
  return (
    <span className="row row-2">
      <Mono>acme/billing</Mono>
      <Sparkline series={[70]} />
      <span className="muted small">one upload so far — nothing to draw</span>
    </span>
  );
}
