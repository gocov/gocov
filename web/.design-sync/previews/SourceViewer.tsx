import { SourceViewer } from "gocov-web";

interface Line {
  no: number;
  text: string;
  /** null = not a statement, 0 = never executed. */
  hits: number | null;
  new_miss: boolean;
}

const line = (no: number, hits: number | null, text: string, newMiss = false): Line => ({
  no,
  text,
  hits,
  new_miss: newMiss,
});

/** internal/core/report.go — covered, uncovered and non-statement lines mixed. */
const report: Line[] = [
  line(1, null, "package core"),
  line(2, null, ""),
  line(3, 142, "func (r *Report) Coverage() float64 {"),
  line(4, 142, "\tif r.TotalStmts == 0 {"),
  line(5, 0, "\t\treturn 0", true),
  line(6, null, "\t}"),
  line(7, 140, "\treturn float64(r.CoveredStmts) / float64(r.TotalStmts) * 100"),
  line(8, null, "}"),
  line(9, null, ""),
  line(10, 37, "func (r *Report) Merge(other *Report) error {"),
  line(11, 37, "\tif other == nil {"),
  line(12, 0, "\t\treturn errors.New(\"gocov: nothing to merge\")", true),
  line(13, null, "\t}"),
  ...Array.from({ length: 11 }, (_, i) => line(14 + i, 36, `\tr.Files[${i}].merge(other.Files[${i}])`)),
  line(25, null, ""),
  line(26, 4, "\tif r.Commit != other.Commit {"),
  line(27, 0, "\t\treturn fmt.Errorf(\"gocov: commit mismatch: %s\", other.Commit)"),
  line(28, null, "\t}"),
  line(29, 36, "\treturn nil"),
  line(30, null, "}"),
];

export function AnnotatedFile() {
  return <SourceViewer lines={report as never} newlyUncovered={2} />;
}

export function FullyCovered() {
  const covered: Line[] = [
    line(1, null, "package detect"),
    line(2, null, ""),
    line(3, 96, "func Format(head []byte) (string, bool) {"),
    line(4, 96, "\tif bytes.HasPrefix(head, []byte(\"mode:\")) {"),
    line(5, 12, "\t\treturn \"go\", true"),
    line(6, null, "\t}"),
    line(7, 84, "\tif bytes.HasPrefix(head, []byte(\"TN:\")) {"),
    line(8, 21, "\t\treturn \"lcov\", true"),
    line(9, null, "\t}"),
    line(10, 63, "\treturn \"\", false"),
    line(11, null, "}"),
  ];
  return <SourceViewer lines={covered as never} newlyUncovered={0} />;
}

export function MostlyUncovered() {
  const untested: Line[] = [
    line(1, null, "package forge"),
    line(2, null, ""),
    line(3, 2, "func (c *Client) PostComment(ctx context.Context, pr int, body string) error {"),
    line(4, 0, "\treq, err := c.request(ctx, \"POST\", c.commentsURL(pr), body)"),
    line(5, 0, "\tif err != nil {"),
    line(6, 0, "\t\treturn err"),
    line(7, null, "\t}"),
    line(8, 0, "\tresp, err := c.http.Do(req)"),
    line(9, 0, "\tif err != nil {"),
    line(10, 0, "\t\treturn fmt.Errorf(\"gocov: post comment: %w\", err)"),
    line(11, null, "\t}"),
    line(12, 0, "\tdefer resp.Body.Close()"),
    line(13, null, ""),
    line(14, 0, "\tif resp.StatusCode >= 400 {"),
    line(15, 0, "\t\treturn statusError(resp)"),
    line(16, null, "\t}"),
    line(17, 0, "\treturn nil"),
    line(18, null, "}"),
  ];
  return <SourceViewer lines={untested as never} newlyUncovered={0} />;
}
