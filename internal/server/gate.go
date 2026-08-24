// The coverage gate: the rule set a repo can require of an upload, and
// the verdict it produces. Evaluation lives here; the same verdict is
// rendered for the web UI in pages.go and pushed to the forge in
// forgepush.go.
package server

import (
	"fmt"
	"strings"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/store"
)

// gateResult is the evaluated coverage gate for one upload.
type gateResult struct {
	configured bool
	failures   []string
}

func (g gateResult) failed() bool { return len(g.failures) > 0 }

func (g gateResult) String() string {
	if g.failed() {
		return "failed: " + strings.Join(g.failures, "; ")
	}
	return "passed"
}

// gateEpsilon absorbs float64 division error so coverage exactly at the
// configured threshold never fails the gate (57 of 100 statements is
// 56.999999999999993 in float arithmetic).
const gateEpsilon = 1e-9

// evaluateGate checks the repo's coverage requirements. dropDelta is the
// difference to the latest gate-passing upload on the default branch —
// never a gate-failing upload, so re-running CI cannot launder a failure,
// and never the branch's own history, so a PR cannot ratchet coverage
// down within tolerance push by push. The drop and diff rules are
// fail-open when their inputs are unavailable.
func evaluateGate(gate store.Gate, totalPct float64, dropDelta *float64, diff *diffcov.Result) gateResult {
	res := gateResult{configured: gate.Configured()}
	if gate.MinCoverage != nil && totalPct < *gate.MinCoverage-gateEpsilon {
		res.failures = append(res.failures,
			fmt.Sprintf("total coverage %.4g%% is below the minimum %.4g%%", totalPct, *gate.MinCoverage))
	}
	if gate.MaxCoverageDrop != nil && dropDelta != nil && *dropDelta < -*gate.MaxCoverageDrop-gateEpsilon {
		res.failures = append(res.failures,
			fmt.Sprintf("coverage dropped %.4g%% (allowed %.4g%%)", -*dropDelta, *gate.MaxCoverageDrop))
	}
	if gate.MinDiffCoverage != nil && diff != nil && diff.TotalLines > 0 && diff.Percent() < *gate.MinDiffCoverage-gateEpsilon {
		res.failures = append(res.failures,
			fmt.Sprintf("diff coverage %.4g%% is below the minimum %.4g%%", diff.Percent(), *gate.MinDiffCoverage))
	}
	return res
}

// gateReason narrates the gate: one clause per configured rule, comparing this
// upload's measured value to the threshold, joined into a sentence. It reads
// the same whether the gate passed or failed — the clauses themselves say
// which rule is the problem.
// subject names the thing being described in the fallback sentences (e.g.
// "This upload", "The latest commit on this branch") so the same narration
// serves both the upload page and the repo page. totalPct/diff are the
// measured values; baseTotal is the baseline's total coverage, valid only
// when hasBase is true.
func gateReason(totalPct float64, diff *diffcov.Result, g store.Gate, baseTotal float64, hasBase bool, subject string) string {
	if !g.Configured() {
		return fmt.Sprintf("No coverage gate is configured for this repo. %s records %.1f%% total coverage.", subject, totalPct)
	}
	var parts []string
	if g.MinCoverage != nil {
		rel := "is above"
		if totalPct < *g.MinCoverage-gateEpsilon {
			rel = "is below"
		}
		parts = append(parts, fmt.Sprintf("total coverage %s the minimum of %.4g%%", rel, *g.MinCoverage))
	}
	if g.MaxCoverageDrop != nil && hasBase {
		drop := baseTotal - totalPct
		if drop <= gateEpsilon {
			parts = append(parts, "coverage held or rose against the base")
		} else {
			rel := "under"
			if drop > *g.MaxCoverageDrop+gateEpsilon {
				rel = "over"
			}
			parts = append(parts, fmt.Sprintf("the drop against the base is %.4g%% — %s the %.4g%% allowed", drop, rel, *g.MaxCoverageDrop))
		}
	}
	if g.MinDiffCoverage != nil && diff != nil && diff.TotalLines > 0 {
		rel := "meets"
		if diff.Percent() < *g.MinDiffCoverage-gateEpsilon {
			rel = "is below"
		}
		parts = append(parts, fmt.Sprintf("diff coverage %s the %.4g%% minimum", rel, *g.MinDiffCoverage))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%s records %.1f%% total coverage.", subject, totalPct)
	}
	sentence := strings.ToUpper(parts[0][:1]) + parts[0][1:]
	if len(parts) > 1 {
		sentence += ", and " + strings.Join(parts[1:], ", and ")
	}
	return sentence + "."
}
