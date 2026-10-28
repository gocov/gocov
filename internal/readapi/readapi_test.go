package readapi

import (
	"bytes"
	"testing"
)

func TestPrint(t *testing.T) {
	delta := 0.39
	for _, tc := range []struct {
		name string
		rep  Report
		want string
	}{
		{"a PR that left lines untested", Report{
			Repo: Repo{"github", "acme/widgets"}, Commit: "7aa8f0162ab5", Branch: "feature", PRID: "247",
			Parts: 2, Coverage: 88.04, Base: &Base{Commit: "c54dc2c0b7d6"}, Delta: &delta,
			Diff: &Diff{Coverage: 82.6, CoveredLines: 19, TotalLines: 23,
				Uncovered: []File{{Path: "cmd/gocov/oidc.go", Lines: "49-50, 57-58"}}},
			ReportURL: "https://g/u/1114?parts=merged",
		}, "github/acme/widgets @ 7aa8f01 (feature, PR 247)\ncoverage: 88.0% (+0.4% vs c54dc2c), 2 parts\n" +
			"diff coverage: 82.6% (19/23 changed lines)\nuncovered changed lines:\n  cmd/gocov/oidc.go: 49-50, 57-58\n" +
			"report: https://g/u/1114?parts=merged\n"},
		{"a branch build with a failed gate", Report{
			Repo: Repo{"github", "acme/widgets"}, Commit: "abcdef123", Branch: "main",
			Coverage: 61.25, GateFailed: true, ReportURL: "https://g/u/1",
		}, "github/acme/widgets @ abcdef1 (main)\ncoverage: 61.2%, gate failed\nreport: https://g/u/1\n"},
		{"a PR that changed no executable lines but untracked files", Report{
			Repo: Repo{"gitlab", "grp/sub/app"}, Commit: "abc", Branch: "docs", PRID: "9", Coverage: 90,
			Diff:      &Diff{Coverage: 100, UnmatchedFiles: []string{"gen/a.go", "gen/b.go"}},
			ReportURL: "https://g/u/2",
		}, "gitlab/grp/sub/app @ abc (docs, PR 9)\ncoverage: 90.0%\ndiff coverage: no executable lines changed\n" +
			"changed files without coverage data: gen/a.go, gen/b.go\nreport: https://g/u/2\n"},
		{"no branch at all", Report{Repo: Repo{"github", "a/b"}, Commit: "abc", ReportURL: "u"},
			"github/a/b @ abc\ncoverage: 0.0%\nreport: u\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			Print(&out, &tc.rep)
			if out.String() != tc.want {
				t.Errorf("output:\n%s\nwant:\n%s", out.String(), tc.want)
			}
		})
	}
}
