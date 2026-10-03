package core

import (
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/store"
)

func pct(v float64) *float64 { return new(v) }

func TestEvaluateGate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		gate     store.Gate
		totalPct float64
		dropBase *float64
		diff     *diffcov.Result
		want     []string // substrings of the expected failures, nil for a pass
	}{
		{
			name: "no rules configured passes anything",
			gate: store.Gate{}, totalPct: 0,
		},
		{
			name: "total above the minimum",
			gate: store.Gate{MinCoverage: pct(80)}, totalPct: 81,
		},
		{
			name: "total below the minimum",
			gate: store.Gate{MinCoverage: pct(80)}, totalPct: 79.5,
			want: []string{"below the minimum"},
		},
		{
			// 57 of 100 statements is 56.999999999999993 in float64, and
			// a gate set to exactly that must not fail on the arithmetic.
			name: "exactly at the threshold passes",
			gate: store.Gate{MinCoverage: pct(57)}, totalPct: 56.999999999999993,
		},
		{
			name: "drop within tolerance",
			gate: store.Gate{MaxCoverageDrop: pct(1)}, totalPct: 79, dropBase: pct(79.5),
		},
		{
			name: "drop beyond tolerance",
			gate: store.Gate{MaxCoverageDrop: pct(1)}, totalPct: 79, dropBase: pct(81),
			want: []string{"coverage dropped"},
		},
		{
			// No baseline to compare against: the rule cannot fail an
			// upload it knows nothing about.
			name: "drop rule fails open without a baseline",
			gate: store.Gate{MaxCoverageDrop: pct(1)}, totalPct: 10, dropBase: nil,
		},
		{
			name: "diff coverage below the minimum",
			gate: store.Gate{MinDiffCoverage: pct(90)}, totalPct: 99,
			diff: &diffcov.Result{TotalLines: 10, CoveredLines: 5},
			want: []string{"diff coverage"},
		},
		{
			name: "diff rule fails open with no diff",
			gate: store.Gate{MinDiffCoverage: pct(90)}, totalPct: 10, diff: nil,
		},
		{
			// A PR that touches no covered lines has nothing to measure.
			name: "diff rule fails open on an empty diff",
			gate: store.Gate{MinDiffCoverage: pct(90)}, totalPct: 10,
			diff: &diffcov.Result{TotalLines: 0},
		},
		{
			name:     "every rule can fail at once",
			gate:     store.Gate{MinCoverage: pct(90), MaxCoverageDrop: pct(1), MinDiffCoverage: pct(90)},
			totalPct: 50, dropBase: pct(55),
			diff: &diffcov.Result{TotalLines: 10, CoveredLines: 1},
			want: []string{"below the minimum", "coverage dropped", "diff coverage"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateGate(tc.gate, tc.totalPct, tc.dropBase, tc.diff)
			if got.Configured != tc.gate.Configured() {
				t.Errorf("Configured = %v, want %v", got.Configured, tc.gate.Configured())
			}
			if len(got.Failures) != len(tc.want) {
				t.Fatalf("failures = %v, want %d of them", got.Failures, len(tc.want))
			}
			for i, want := range tc.want {
				if !strings.Contains(got.Failures[i], want) {
					t.Errorf("failure %d = %q, want it to mention %q", i, got.Failures[i], want)
				}
			}
			if got.Failed() != (len(tc.want) > 0) {
				t.Errorf("Failed() = %v with failures %v", got.Failed(), got.Failures)
			}
			if want := "passed"; !got.Failed() && got.String() != want {
				t.Errorf("String() = %q, want %q", got.String(), want)
			}
			if got.Failed() && !strings.HasPrefix(got.String(), "failed: ") {
				t.Errorf("String() = %q, want a failed: prefix", got.String())
			}
		})
	}
}

func TestMinCoverageFailed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		gate     store.Gate
		totalPct float64
		want     bool
	}{
		{name: "no minimum configured", gate: store.Gate{MaxCoverageDrop: pct(1)}, totalPct: 0},
		{name: "above the minimum", gate: store.Gate{MinCoverage: pct(80)}, totalPct: 81},
		{name: "exactly at the threshold", gate: store.Gate{MinCoverage: pct(57)}, totalPct: 56.999999999999993},
		{name: "below the minimum", gate: store.Gate{MinCoverage: pct(80)}, totalPct: 79.9, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MinCoverageFailed(tc.gate, tc.totalPct); got != tc.want {
				t.Errorf("MinCoverageFailed = %v, want %v", got, tc.want)
			}
			// The predicate and the verdict must agree on the minimum rule.
			verdict := EvaluateGate(tc.gate, tc.totalPct, nil, nil)
			if verdict.Failed() != tc.want {
				t.Errorf("EvaluateGate failed = %v, MinCoverageFailed = %v", verdict.Failed(), tc.want)
			}
		})
	}
}

func TestGateReason(t *testing.T) {
	for _, tc := range []struct {
		name     string
		gate     store.Gate
		totalPct float64
		dropBase *float64
		diff     *diffcov.Result
		want     string
	}{
		{
			name: "no gate configured",
			gate: store.Gate{}, totalPct: 72.34,
			want: "No coverage gate is configured for this repo. This upload records 72.3% total coverage.",
		},
		{
			name: "total above the minimum",
			gate: store.Gate{MinCoverage: pct(80)}, totalPct: 85,
			want: "Total coverage is above the minimum of 80%.",
		},
		{
			name: "total below the minimum",
			gate: store.Gate{MinCoverage: pct(80)}, totalPct: 70,
			want: "Total coverage is below the minimum of 80%.",
		},
		{
			name: "total exactly at the threshold reads as above",
			gate: store.Gate{MinCoverage: pct(57)}, totalPct: 56.999999999999993,
			want: "Total coverage is above the minimum of 57%.",
		},
		{
			name: "coverage rose against the default branch",
			gate: store.Gate{MaxCoverageDrop: pct(1)}, totalPct: 81, dropBase: pct(80),
			want: "Coverage held or rose against the default branch.",
		},
		{
			name: "drop under the allowance",
			gate: store.Gate{MaxCoverageDrop: pct(1)}, totalPct: 79.5, dropBase: pct(80),
			want: "The drop against the default branch is 0.5% — under the 1% allowed.",
		},
		{
			name: "drop over the allowance",
			gate: store.Gate{MaxCoverageDrop: pct(1)}, totalPct: 77, dropBase: pct(80),
			want: "The drop against the default branch is 3% — over the 1% allowed.",
		},
		{
			// Without a recorded baseline the gate skipped the drop rule, so
			// the narration must not invent a clause for it.
			name: "drop rule without a baseline falls back to the total",
			gate: store.Gate{MaxCoverageDrop: pct(1)}, totalPct: 77,
			want: "This upload records 77.0% total coverage.",
		},
		{
			name: "diff coverage meets the minimum",
			gate: store.Gate{MinDiffCoverage: pct(80)}, totalPct: 50,
			diff: &diffcov.Result{TotalLines: 10, CoveredLines: 9},
			want: "Diff coverage meets the 80% minimum.",
		},
		{
			name: "diff coverage below the minimum",
			gate: store.Gate{MinDiffCoverage: pct(80)}, totalPct: 50,
			diff: &diffcov.Result{TotalLines: 10, CoveredLines: 5},
			want: "Diff coverage is below the 80% minimum.",
		},
		{
			name: "empty diff leaves the diff rule out",
			gate: store.Gate{MinDiffCoverage: pct(80)}, totalPct: 50,
			diff: &diffcov.Result{TotalLines: 0},
			want: "This upload records 50.0% total coverage.",
		},
		{
			name:     "every rule joins into one sentence",
			gate:     store.Gate{MinCoverage: pct(60), MaxCoverageDrop: pct(1), MinDiffCoverage: pct(90)},
			totalPct: 55, dropBase: pct(58),
			diff: &diffcov.Result{TotalLines: 4, CoveredLines: 1},
			want: "Total coverage is below the minimum of 60%, and the drop against the default branch is 3% — over the 1% allowed, and diff coverage is below the 90% minimum.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := GateReason(tc.totalPct, tc.diff, tc.gate, tc.dropBase, "This upload")
			if got != tc.want {
				t.Errorf("GateReason =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}
