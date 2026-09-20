import { Chip, Mono } from "gocov-web";

export function Tones() {
  return (
    <div className="row row-2">
      <Chip tone="good">Gate passing</Chip>
      <Chip tone="warn">Stale</Chip>
      <Chip tone="bad">Gate failing</Chip>
      <Chip tone="accent">3 active</Chip>
      <Chip>No gate</Chip>
      <Chip tone="plain">App install</Chip>
    </div>
  );
}

export function InARepositoryList() {
  return (
    <div className="stack stack-1">
      <span className="row row-2">
        <Mono>acme/api</Mono>
        <Chip tone="good">74.0% · gate passing</Chip>
      </span>
      <span className="row row-2">
        <Mono>acme/web</Mono>
        <Chip tone="warn">No upload in 21 days</Chip>
      </span>
      <span className="row row-2">
        <Mono>acme/billing</Mono>
        <Chip tone="bad">41.4% · below 60%</Chip>
      </span>
      <span className="row row-2">
        <Mono>acme/docs</Mono>
        <Chip>No gate</Chip>
      </span>
    </div>
  );
}

export function PlainLabels() {
  return (
    <div className="row row-2">
      <Chip tone="plain">Owner</Chip>
      <Chip tone="plain">Member</Chip>
      <Chip tone="plain">App install</Chip>
      <Chip tone="plain">go · coverage.out</Chip>
    </div>
  );
}
