package core

import (
	"fmt"
	"testing"

	"github.com/gocov/gocov/internal/store"
)

func TestReportBaseline(t *testing.T) {
	report := func(id int64, pct float64, failed bool) *store.CommitReport {
		return &store.CommitReport{ID: id, UploadID: id, CommitSHA: fmt.Sprintf("c%d", id), TotalPct: pct, GateFailed: failed}
	}
	ok := func(id int64) *store.CommitReport { return report(id, 80, false) }
	failed := func(id int64) *store.CommitReport { return report(id, 50, true) }
	prBuild := func(id int64) *store.CommitReport {
		cr := ok(id)
		cr.PRID = "7"
		return cr
	}
	for _, tc := range []struct {
		name          string
		reports       []*store.CommitReport // newest first
		wantCur, want int64                 // report ids, 0 for nil
	}{
		{"no reports", nil, 0, 0},
		{"single report", []*store.CommitReport{ok(1)}, 1, 0},
		{"previous passed", []*store.CommitReport{ok(2), ok(1)}, 2, 1},
		// A failed report never becomes the baseline; the delta reads
		// against the last one that passed.
		{"skips failed", []*store.CommitReport{ok(3), failed(2), ok(1)}, 3, 1},
		{"all earlier failed", []*store.CommitReport{ok(3), failed(2), failed(1)}, 3, 0},
		// The newest report is the current one whether or not it passed.
		{"current failed", []*store.CommitReport{failed(2), ok(1)}, 2, 1},
		// A feature branch's trend carries its PR's builds; the newest one
		// is current, but none is a baseline.
		{"skips PR builds", []*store.CommitReport{prBuild(3), prBuild(2), ok(1)}, 3, 1},
		{"only PR builds", []*store.CommitReport{prBuild(2), prBuild(1)}, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := func(r *store.CommitReport) int64 {
				if r == nil {
					return 0
				}
				return r.ID
			}
			cur, base := ReportBaseline(tc.reports)
			if id(cur) != tc.wantCur || id(base) != tc.want {
				t.Errorf("ReportBaseline = %d, %d, want %d, %d", id(cur), id(base), tc.wantCur, tc.want)
			}
		})
	}
}
