import { Chip, KeyValue, KeyValueList, Mono, VerdictCard } from "gocov-web";

const commitMeta = (
  <KeyValueList>
    <KeyValue
      label="Commit"
      value={
        <>
          <a href="/uploads/412">
            <Mono>a1b2c3d4e5f6</Mono>
          </a>{" "}
          <span className="muted small">&middot; 3 hours ago</span>
        </>
      }
    />
    <KeyValue
      label="Branch"
      value={
        <span className="row">
          <Mono>main</Mono> <Chip tone="plain">default</Chip>
        </span>
      }
    />
    <KeyValue label="Statements" value={<>9,236/11,224 <span className="muted small">covered</span></>} />
  </KeyValueList>
);

export function GatePassing() {
  return (
    <VerdictCard
      verdict={{
        state: "pass",
        coverage: 82.3,
        delta: 1.4,
        reason: "Total coverage 82.3% is at or above the minimum of 80%. Diff coverage 91.4% is at or above 70%.",
        base: { upload_id: 408, sha: "5e0b9912aa47c630", coverage: 80.9 },
      }}
      meta={commitMeta}
    />
  );
}

export function GateFailing() {
  return (
    <VerdictCard
      verdict={{
        state: "fail",
        coverage: 61.0,
        delta: -2.2,
        reason:
          "Total coverage 61.0% is below the minimum of 80%. Coverage dropped 2.2 points against a1b2c3d4e5f6, more than the 1.0 point allowed.",
        base: { upload_id: 412, sha: "a1b2c3d4e5f67890", coverage: 63.2 },
      }}
      meta={
        <KeyValueList>
          <KeyValue
            label="Commit"
            value={
              <>
                <a href="/uploads/415">
                  <Mono>8b41c07de9f3</Mono>
                </a>{" "}
                <span className="muted small">&middot; 6 hours ago</span>
              </>
            }
          />
          <KeyValue
            label="Branch"
            value={
              <span className="row">
                <Mono>fix/upload-retry</Mono> <Chip tone="accent">PR #128</Chip>
              </span>
            }
          />
          <KeyValue
            label="Base"
            value={
              <>
                <a href="/uploads/412">
                  <Mono>a1b2c3d4e5f6</Mono>
                </a>{" "}
                <span className="muted small">&middot; 63.2%</span>
              </>
            }
          />
        </KeyValueList>
      }
    />
  );
}

export function NoGateConfigured() {
  return (
    <VerdictCard
      verdict={{
        state: "neutral",
        coverage: 74.0,
        delta: null,
        reason: "No gate is configured for acme/web, so this report is recorded without a verdict.",
        base: null,
      }}
      meta={
        <KeyValueList>
          <KeyValue
            label="Commit"
            value={
              <>
                <a href="/uploads/307">
                  <Mono>3ab77cc10de9</Mono>
                </a>{" "}
                <span className="muted small">&middot; yesterday</span>
              </>
            }
          />
          <KeyValue label="Branch" value={<Mono>main</Mono>} />
        </KeyValueList>
      }
    />
  );
}

export function WithoutMeta() {
  return (
    <VerdictCard
      verdict={{
        state: "pass",
        coverage: 91.4,
        delta: 0.6,
        reason: "Total coverage 91.4% is at or above the minimum of 90%.",
        base: { upload_id: 410, sha: "7c1de90fa2b41188", coverage: 90.8 },
      }}
    />
  );
}
