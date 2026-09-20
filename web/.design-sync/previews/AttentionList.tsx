import { AttentionList } from "gocov-web";

const failing = {
  key: "failing:github/acme/api",
  tone: "bad" as const,
  status: "Failing",
  before: "",
  name: "api",
  after: " is failing its coverage gate",
  message: "Coverage 41.0%, below the 60% minimum.",
  action: "Open repo",
  to: "/repos/github/acme/api",
};

const stale = {
  key: "stale:github/acme/web",
  tone: "warn" as const,
  status: "Stale",
  before: "No uploads from ",
  name: "web",
  after: " in 21 days",
  message: "Its last pipeline run did not reach the upload step; the coverage shown is stale.",
  action: "Open repo",
  to: "/repos/github/acme/web",
};

export function WhatNeedsAttention() {
  return (
    <AttentionList
      rows={[
        failing,
        {
          ...failing,
          key: "failing:gitlab/acme/billing",
          name: "billing",
          message: "Coverage 68.4%, below the 75% minimum.",
          to: "/repos/gitlab/acme/billing",
        },
        stale,
      ]}
    />
  );
}

export function AFailingGate() {
  return <AttentionList rows={[failing]} />;
}

export function StaleUploads() {
  return (
    <AttentionList
      rows={[
        stale,
        {
          ...stale,
          key: "stale:bitbucket/acme/worker",
          name: "worker",
          after: " in 9 days",
          to: "/repos/bitbucket/acme/worker",
        },
      ]}
    />
  );
}

export function NothingToAttendTo() {
  return (
    <p className="muted small">
      Every repository in acme is passing its gate and uploading. With no notices the list renders nothing at all:{" "}
      <AttentionList rows={[]} />
    </p>
  );
}
