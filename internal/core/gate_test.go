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
		drop     *float64
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
			gate: store.Gate{MaxCoverageDrop: pct(1)}, totalPct: 79, drop: pct(-0.5),
		},
		{
			name: "drop beyond tolerance",
			gate: store.Gate{MaxCoverageDrop: pct(1)}, totalPct: 79, drop: pct(-2),
			want: []string{"coverage dropped"},
		},
		{
			// No baseline to compare against: the rule cannot fail an
			// upload it knows nothing about.
			name: "drop rule fails open without a baseline",
			gate: store.Gate{MaxCoverageDrop: pct(1)}, totalPct: 10, drop: nil,
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
			totalPct: 50, drop: pct(-5),
			diff: &diffcov.Result{TotalLines: 10, CoveredLines: 1},
			want: []string{"below the minimum", "coverage dropped", "diff coverage"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateGate(tc.gate, tc.totalPct, tc.drop, tc.diff)
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
