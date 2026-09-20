import { InlineCode } from "gocov-web";

export function InASentence() {
  return (
    <p>
      Set <InlineCode>GOCOV_TOKEN</InlineCode> in your CI secrets, then add{" "}
      <InlineCode>gocov-action@v1</InlineCode> after the step that writes{" "}
      <InlineCode>coverage.out</InlineCode>.
    </p>
  );
}

export function InAHelpList() {
  return (
    <div className="stack stack-1">
      <span>
        Gates are read from <InlineCode>.gocov.yml</InlineCode> at the root of the default branch.
      </span>
      <span>
        Patterns such as <InlineCode>vendor/**</InlineCode> and <InlineCode>**/*_test.go</InlineCode> are
        ignored before coverage is computed.
      </span>
      <span>
        The upload is signed for the commit <InlineCode>a1b2c3d</InlineCode> on{" "}
        <InlineCode>main</InlineCode>.
      </span>
    </div>
  );
}

export function LongValuesWrap() {
  return (
    <p>
      A self-hosted instance points the CLI at its own server with{" "}
      <InlineCode>GOCOV_SERVER=https://gocov.example.com</InlineCode>, and uploads with{" "}
      <InlineCode>gocov upload --token $GOCOV_TOKEN --branch main coverage.out</InlineCode>.
    </p>
  );
}
