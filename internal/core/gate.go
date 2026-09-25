// The coverage gate: the rules a repo can require of a commit, the
// verdict they produce, and the sentence that explains it to a human.

package core

import (
	"fmt"
	"strings"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/store"
)

// Verdict is the evaluated coverage gate for one upload.
type Verdict struct {
	Configured bool
	Failures   []string
}

func (v Verdict) Failed() bool { return len(v.Failures) > 0 }

func (v Verdict) String() string {
	if v.Failed() {
		return "failed: " + strings.Join(v.Failures, "; ")
	}
	return "passed"
}

// gateEpsilon absorbs float64 division error so coverage exactly at the
// configured threshold never fails the gate (57 of 100 statements is
// 56.999999999999993 in float arithmetic).
const gateEpsilon = 1e-9

// Each rule's comparison lives in one predicate, shared by the verdict
// and the sentence explaining it, so the two cannot disagree.

// belowMin reports whether a measured percentage misses a minimum.
func belowMin(measured, minimum float64) bool { return measured < minimum-gateEpsilon }

// dropOver reports whether a coverage drop exceeds what the gate allows.
func dropOver(drop, allowed float64) bool { return drop > allowed+gateEpsilon }

// diffMeasured reports whether there is diff coverage to judge: a PR that
// touches no covered lines has nothing to measure.
func diffMeasured(diff *diffcov.Result) bool { return diff != nil && diff.TotalLines > 0 }

// EvaluateGate checks the repo's coverage requirements. dropBase is the
// total of the gate's drop baseline (gateDropBase): the latest gate-passing
// report on the default branch — never a gate-failing one, so re-running
// CI cannot launder a failure, and never the branch's own history, so a PR
// cannot ratchet coverage down within tolerance push by push. The drop and
// diff rules are fail-open when their inputs are unavailable.
func EvaluateGate(gate store.Gate, totalPct float64, dropBase *float64, diff *diffcov.Result) Verdict {
	res := Verdict{Configured: gate.Configured()}
	if gate.MinCoverage != nil && belowMin(totalPct, *gate.MinCoverage) {
		res.Failures = append(res.Failures,
			fmt.Sprintf("total coverage %.4g%% is below the minimum %.4g%%", totalPct, *gate.MinCoverage))
	}
	if gate.MaxCoverageDrop != nil && dropBase != nil && dropOver(*dropBase-totalPct, *gate.MaxCoverageDrop) {
		res.Failures = append(res.Failures,
			fmt.Sprintf("coverage dropped %.4g%% (allowed %.4g%%)", *dropBase-totalPct, *gate.MaxCoverageDrop))
	}
	if gate.MinDiffCoverage != nil && diffMeasured(diff) && belowMin(diff.Percent(), *gate.MinDiffCoverage) {
		res.Failures = append(res.Failures,
			fmt.Sprintf("diff coverage %.4g%% is below the minimum %.4g%%", diff.Percent(), *gate.MinDiffCoverage))
	}
	return res
}

// GateReason narrates the Verdict: one clause per configured rule, comparing
// the measured value to the threshold, joined into a sentence. It reads the
// same whether the gate passed or failed — the clauses themselves say which
// rule is the problem.
//
// subject names the thing being described in the fallback sentences (e.g.
// "This upload", "The latest commit") so the same narration serves the
// upload page and the repo page. dropBase is the drop baseline the gate
// was evaluated against, as recorded with the row (GateBasePct); nil leaves
// the drop rule out, as the gate itself did.
func GateReason(totalPct float64, diff *diffcov.Result, g store.Gate, dropBase *float64, subject string) string {
	if !g.Configured() {
		return fmt.Sprintf("No coverage gate is configured for this repo. %s records %.1f%% total coverage.", subject, totalPct)
	}
	var parts []string
	if g.MinCoverage != nil {
		rel := "is above"
		if belowMin(totalPct, *g.MinCoverage) {
			rel = "is below"
		}
		parts = append(parts, fmt.Sprintf("total coverage %s the minimum of %.4g%%", rel, *g.MinCoverage))
	}
	if g.MaxCoverageDrop != nil && dropBase != nil {
		drop := *dropBase - totalPct
		if drop <= gateEpsilon {
			parts = append(parts, "coverage held or rose against the default branch")
		} else {
			rel := "under"
			if dropOver(drop, *g.MaxCoverageDrop) {
				rel = "over"
			}
			parts = append(parts, fmt.Sprintf("the drop against the default branch is %.4g%% — %s the %.4g%% allowed", drop, rel, *g.MaxCoverageDrop))
		}
	}
	if g.MinDiffCoverage != nil && diffMeasured(diff) {
		rel := "meets"
		if belowMin(diff.Percent(), *g.MinDiffCoverage) {
			rel = "is below"
		}
		parts = append(parts, fmt.Sprintf("diff coverage %s the %.4g%% minimum", rel, *g.MinDiffCoverage))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%s records %.1f%% total coverage.", subject, totalPct)
	}
	sentence := strings.Join(parts, ", and ")
	return strings.ToUpper(sentence[:1]) + sentence[1:] + "."
}
