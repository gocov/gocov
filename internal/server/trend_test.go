package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/store"
)

// trendReport builds a merged commit report for newTrendView tests. Callers
// pass reports newest-first, as ListBranchCommitReports returns them. Each
// report gets a distinct commit so the upload-id link map is unambiguous.
func trendReport(id int64, pct float64, prID string, gateFailed bool) *store.CommitReport {
	return &store.CommitReport{
		ID: id, CommitSHA: fmt.Sprintf("c%d", id), Branch: "main",
		PRID: prID, TotalPct: pct, GateFailed: gateFailed,
		CreatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(id) * time.Hour),
	}
}

// trendIDs maps each report's commit to an upload id equal to its report id,
// so point.ID assertions read the same as when the trend ran off uploads.
func trendIDs(reports ...*store.CommitReport) map[string]int64 {
	m := make(map[string]int64, len(reports))
	for _, cr := range reports {
		m[cr.CommitSHA] = cr.ID
	}
	return m
}

func TestTrendViewTooFewUploads(t *testing.T) {
	if v := newTrendView("main", nil, nil); v != nil {
		t.Errorf("no reports: got %+v, want nil", v)
	}
	one := []*store.CommitReport{trendReport(1, 80, "", false)}
	if v := newTrendView("main", one, trendIDs(one...)); v != nil {
		t.Errorf("one report: got %+v, want nil", v)
	}
	// Two reports but one is a PR build: one point left, still no chart.
	reports := []*store.CommitReport{trendReport(2, 90, "7", false), trendReport(1, 80, "", false)}
	if v := newTrendView("main", reports, trendIDs(reports...)); v != nil {
		t.Errorf("one non-PR report: got %+v, want nil", v)
	}
}

func TestTrendViewExcludesPRUploads(t *testing.T) {
	reports := []*store.CommitReport{ // newest first
		trendReport(3, 90, "", false),
		trendReport(2, 50, "7", false), // PR build, must not appear
		trendReport(1, 80, "", false),
	}
	v := newTrendView("main", reports, trendIDs(reports...))
	if v == nil {
		t.Fatal("nil view")
	}
	if len(v.Points) != 2 {
		t.Fatalf("points = %d, want 2", len(v.Points))
	}
	for _, p := range v.Points {
		if p.ID == 2 {
			t.Errorf("PR report leaked into the trend: %+v", p)
		}
	}
}

func TestTrendViewPointPlacement(t *testing.T) {
	reports := []*store.CommitReport{ // newest first: chronological pcts are 70, 75, 80
		trendReport(3, 80, "", false),
		trendReport(2, 75, "", false),
		trendReport(1, 70, "", false),
	}
	v := newTrendView("main", reports, trendIDs(reports...))
	if v == nil {
		t.Fatal("nil view")
	}
	// Chronological left to right: oldest upload at the left edge.
	if v.Points[0].ID != 1 || v.Points[2].ID != 3 {
		t.Errorf("point order = %d,%d,%d, want 1,2,3", v.Points[0].ID, v.Points[1].ID, v.Points[2].ID)
	}
	// The template hangs gridlines and axis labels off the plot edges.
	if v.X0 != 46 || v.X1 != 786 {
		t.Errorf("plot edges = %d,%d, want 46,786", v.X0, v.X1)
	}
	// X spans the plot area evenly: 46, 416, 786.
	for i, want := range []float64{46, 416, 786} {
		if v.Points[i].X != want {
			t.Errorf("point %d X = %g, want %g", i, v.Points[i].X, want)
		}
	}
	// Y: range 70-80 padded by 1 -> 69..81 over 124px below y=12.
	// y(70) = 12 + 124*11/12 = 125.7; y(75) = 74 (middle); y(80) = 22.3.
	for i, want := range []float64{125.7, 74, 22.3} {
		if v.Points[i].Y != want {
			t.Errorf("point %d Y = %g, want %g", i, v.Points[i].Y, want)
		}
	}
	if want := "M46 125.7 L416 74 L786 22.3"; v.Path != want {
		t.Errorf("path = %q, want %q", v.Path, want)
	}
	// Min and max gridlines labeled with the data's extremes.
	if len(v.Grid) != 2 || v.Grid[0].Label != "80.0%" || v.Grid[1].Label != "70.0%" {
		t.Errorf("grid = %+v, want labeled 80.0%% and 70.0%%", v.Grid)
	}
	if v.CurLabel != "80.0%" {
		t.Errorf("current label = %q, want 80.0%%", v.CurLabel)
	}
	if v.FirstDate != "2026-08-01" || v.LastDate != "2026-08-01" {
		t.Errorf("dates = %q..%q", v.FirstDate, v.LastDate)
	}
}

func TestTrendViewFlatSeries(t *testing.T) {
	reports := []*store.CommitReport{trendReport(2, 75, "", false), trendReport(1, 75, "", false)}
	v := newTrendView("main", reports, trendIDs(reports...))
	if v == nil {
		t.Fatal("nil view")
	}
	// A flat series pads ±0.5 so the line sits mid-chart, with one gridline.
	if len(v.Grid) != 1 || v.Grid[0].Label != "75.0%" {
		t.Errorf("grid = %+v, want a single 75.0%% line", v.Grid)
	}
	if v.Points[0].Y != v.Points[1].Y {
		t.Errorf("flat series not flat: %g vs %g", v.Points[0].Y, v.Points[1].Y)
	}
}

func TestTrendViewGateMarkers(t *testing.T) {
	reports := []*store.CommitReport{
		trendReport(2, 60, "", true),
		trendReport(1, 80, "", false),
	}
	v := newTrendView("main", reports, trendIDs(reports...))
	if v == nil {
		t.Fatal("nil view")
	}
	if v.Points[0].GateFailed || !v.Points[1].GateFailed {
		t.Errorf("gate flags = %v,%v, want false,true", v.Points[0].GateFailed, v.Points[1].GateFailed)
	}
}

func TestTrendViewScaleClampsAt100(t *testing.T) {
	reports := []*store.CommitReport{trendReport(2, 100, "", false), trendReport(1, 99, "", false)}
	v := newTrendView("main", reports, trendIDs(reports...))
	if v == nil {
		t.Fatal("nil view")
	}
	// Padding must not push the scale past 100: the 100% point sits at the
	// top gridline, not above it.
	if v.Points[1].Y != v.Grid[0].Y {
		t.Errorf("100%% point Y = %g, top gridline Y = %g", v.Points[1].Y, v.Grid[0].Y)
	}
	if v.Grid[0].Label != "100.0%" {
		t.Errorf("top gridline label = %q", v.Grid[0].Label)
	}
}
