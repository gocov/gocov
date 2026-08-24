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
