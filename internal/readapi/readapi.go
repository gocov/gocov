// Package readapi is the read API's contract: the answer GET
// /api/v1/reports/… gives for one commit, which the server builds and the
// CLI decodes, and its plain-text spelling, which gocov report and both MCP
// servers (gocov mcp, the server's /mcp) print. One definition, so the
// three cannot drift apart. docs/api.md documents it.
package readapi

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Report is the read API's answer for one commit. Field names are part of
// the contract: add fields, never rename or repurpose them.
type Report struct {
	Repo   Repo   `json:"repo"`
	Commit string `json:"commit"`
	Branch string `json:"branch"`
	PRID   string `json:"pr_id,omitempty"`
	// UpdatedAt is the commit's latest upload: the report changes until
	// every part has arrived.
	UpdatedAt         time.Time `json:"updated_at"`
	Parts             int       `json:"parts"`
	Coverage          float64   `json:"coverage"`
	CoveredStatements int64     `json:"covered_statements"`
	TotalStatements   int64     `json:"total_statements"`
	GateFailed        bool      `json:"gate_failed"`
	// Base and Delta are null when the commit has nothing to compare with.
	Base  *Base    `json:"base"`
	Delta *float64 `json:"delta"`
	// Diff is null for a commit that is not a pull-request build, or whose
	// diff could not be read.
	Diff      *Diff  `json:"diff"`
	ReportURL string `json:"report_url"`
}

type Repo struct {
	Forge string `json:"forge"`
	Slug  string `json:"slug"`
}

type Base struct {
	Commit   string  `json:"commit"`
	Coverage float64 `json:"coverage"`
}

type Diff struct {
	Coverage     float64 `json:"coverage"`
	CoveredLines int64   `json:"covered_lines"`
	TotalLines   int64   `json:"total_lines"`
	// Uncovered lists the changed files with changed lines no test ran,
	// in diff order. Never null, so a client can range over it.
	Uncovered []File `json:"uncovered"`
	// UnmatchedFiles are changed files the coverage data does not mention.
	UnmatchedFiles []string `json:"unmatched_files"`
}

type File struct {
	Path string `json:"path"`
	// Lines is the human spelling, "49-50, 57-58"; Spans the same lines
	// as inclusive [start, end] pairs.
	Lines string   `json:"lines"`
	Spans [][2]int `json:"spans"`
}

// Print writes the report as a few plain lines: easy to read, and easy for
// an agent to act on line by line.
func Print(w io.Writer, r *Report) {
	head := fmt.Sprintf("%s/%s @ %s", r.Repo.Forge, r.Repo.Slug, Short(r.Commit))
	if r.PRID != "" {
		head += fmt.Sprintf(" (%s, PR %s)", r.Branch, r.PRID)
	} else if r.Branch != "" {
		head += fmt.Sprintf(" (%s)", r.Branch)
	}
	fmt.Fprintln(w, head)

	cov := fmt.Sprintf("coverage: %.1f%%", r.Coverage)
	if r.Delta != nil && r.Base != nil {
		cov += fmt.Sprintf(" (%+.1f%% vs %s)", *r.Delta, Short(r.Base.Commit))
	}
	if r.Parts > 1 {
		cov += fmt.Sprintf(", %d parts", r.Parts)
	}
	if r.GateFailed {
		cov += ", gate failed"
	}
	fmt.Fprintln(w, cov)

	if d := r.Diff; d != nil {
		if d.TotalLines == 0 {
			fmt.Fprintln(w, "diff coverage: no executable lines changed")
		} else {
			fmt.Fprintf(w, "diff coverage: %.1f%% (%d/%d changed lines)\n", d.Coverage, d.CoveredLines, d.TotalLines)
		}
		if len(d.Uncovered) > 0 {
			fmt.Fprintln(w, "uncovered changed lines:")
			for _, f := range d.Uncovered {
				fmt.Fprintf(w, "  %s: %s\n", f.Path, f.Lines)
			}
		}
		if len(d.UnmatchedFiles) > 0 {
			fmt.Fprintf(w, "changed files without coverage data: %s\n", strings.Join(d.UnmatchedFiles, ", "))
		}
	}
	fmt.Fprintf(w, "report: %s\n", r.ReportURL)
}

// Short is a commit SHA's seven-character spelling.
func Short(sha string) string { return sha[:min(len(sha), 7)] }
