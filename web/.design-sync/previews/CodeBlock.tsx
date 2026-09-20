import { CodeBlock } from "gocov-web";

export function GitHubActions() {
  return (
    <CodeBlock label="GitHub Actions step">
      {"- uses: gocov/gocov-action@v1\n  with:\n    files: coverage.out\n    token: ${{ secrets.GOCOV_TOKEN }}"}
    </CodeBlock>
  );
}

export function ASingleCommand() {
  return <CodeBlock label="Upload from any CI">{"gocov upload --token $GOCOV_TOKEN coverage.out"}</CodeBlock>;
}

export function GitLabComponent() {
  return (
    <CodeBlock label=".gitlab-ci.yml">
      {
        "include:\n  - component: gitlab.com/gocov/gocov/upload@1.1.0\n    inputs:\n      files: coverage.xml\n      stage: test"
      }
    </CodeBlock>
  );
}

export function ScrollsSideways() {
  return (
    <CodeBlock label="One long line">
      {"go test ./... -coverprofile=coverage.out -covermode=atomic && gocov upload --token $GOCOV_TOKEN --branch main coverage.out"}
    </CodeBlock>
  );
}
