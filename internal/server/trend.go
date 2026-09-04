package server

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/gocov/gocov/internal/store"
)

// The trend chart plots total coverage over the branch's recent uploads
// as server-rendered inline SVG. Geometry lives here so the template only
// places precomputed coordinates.
const (
	trendReportLimit = 60

	trendW, trendH       = 800, 160
	trendPadL, trendPadR = 46, 14
	trendPadT, trendPadB = 12, 24
)

// trendPoint is one upload's marker on the chart.
type trendPoint struct {
	X, Y       float64
	ID         int64
	GateFailed bool
	Title      string // hover text: "2026-08-05 · 82.3% · abc123def456"
}

// trendGridLine is a labeled horizontal gridline (the series min and max).
type trendGridLine struct {
	Y     float64
	Label string
}

// trendThreshold marks the gate minimum on the chart, so the reader sees how
// the series sits against the line that decides pass/fail.
type trendThreshold struct {
	Y     float64
	Label string
}

// trendView is the fully computed chart handed to the repo template.
type trendView struct {
	W, H       int
	X0, X1     int // plot's left/right edge; gridlines and axis labels hang off these
	Branch     string
	Path       string // the polyline through every point
	Points     []trendPoint
	Grid       []trendGridLine
	Thresh     *trendThreshold // gate minimum line; nil when no gate is set
	CurLabel   string          // current (latest) percentage, drawn at the last point
	CurX, CurY float64
	FirstDate  string
	LastDate   string
}

// round1 keeps SVG coordinates at one decimal so output stays compact and
// deterministic.
func round1(v float64) float64 { return math.Round(v*10) / 10 }

// linePath accumulates an SVG polyline — "M x y L x y …" — one point at a
// time, so the chart and the dashboard's sparklines draw a series the same
// way.
type linePath struct{ strings.Builder }

func (p *linePath) lineTo(x, y float64) {
	cmd := " L"
	if p.Len() == 0 {
		cmd = "M"
	}
	fmt.Fprintf(p, "%s%g %g", cmd, x, y)
}

// newTrendView builds the chart for a branch from its merged commit
// reports, given newest-first as ListBranchCommitReports returns them. PR
// reports are excluded: the branch trend reflects the branch's own commits.
// Each report carries the upload id it links to (UploadID). Returns nil when
// fewer than two points remain — the page then omits the section. The gate
// minimum, when the repo has one, is drawn as a dashed threshold line and
// folded into the plotted range so it is always visible.
func newTrendView(branch string, reports []*store.CommitReport, gateMin *float64) *trendView {
	var series []*store.CommitReport // chronological
	for _, report := range slices.Backward(reports) {
		if report.PRID == "" {
			series = append(series, report)
		}
	}
	if len(series) < 2 {
		return nil
	}

	// Y auto-scales to the data's range with padding, not 0-100: a 72-78%
	// story must not flatline. Clamped so the padding never invents
	// impossible percentages.
	lo, hi := series[0].TotalPct, series[0].TotalPct
	for _, u := range series[1:] {
		lo, hi = min(lo, u.TotalPct), max(hi, u.TotalPct)
	}
	pad := max((hi-lo)*0.1, 0.5)
	yLo := max(0, lo-pad)
	yHi := min(100, hi+pad)

	// A configured gate minimum widens the plotted range so its line always
	// lands on the canvas — the grid labels still read the series min/max, so
	// the gate line reads as a separate reference, not a data point.
	if gateMin != nil {
		yLo = min(yLo, max(0, *gateMin))
		yHi = max(yHi, min(100, *gateMin))
	}

	plotW := float64(trendW - trendPadL - trendPadR)
	plotH := float64(trendH - trendPadT - trendPadB)
	x := func(i int) float64 {
		return round1(trendPadL + plotW*float64(i)/float64(len(series)-1))
	}
	y := func(pct float64) float64 {
		return round1(trendPadT + plotH*(yHi-pct)/(yHi-yLo))
	}

	v := &trendView{
		W:         trendW,
		H:         trendH,
		X0:        trendPadL,
		X1:        trendW - trendPadR,
		Branch:    branch,
		FirstDate: series[0].CreatedAt.Format("2006-01-02"),
		LastDate:  series[len(series)-1].CreatedAt.Format("2006-01-02"),
	}
	var path linePath
	for i, u := range series {
		px, py := x(i), y(u.TotalPct)
		path.lineTo(px, py)
		v.Points = append(v.Points, trendPoint{
			X: px, Y: py, ID: u.UploadID, GateFailed: u.GateFailed,
			Title: fmt.Sprintf("%s · %.1f%% · %s", u.CreatedAt.Format("2006-01-02"), u.TotalPct, shortSHA(u.CommitSHA)),
		})
	}
	v.Path = path.String()

	v.Grid = []trendGridLine{{Y: y(hi), Label: fmt.Sprintf("%.1f%%", hi)}}
	if lo != hi {
		v.Grid = append(v.Grid, trendGridLine{Y: y(lo), Label: fmt.Sprintf("%.1f%%", lo)})
	}

	if gateMin != nil {
		v.Thresh = &trendThreshold{Y: y(*gateMin), Label: fmt.Sprintf("gate %.4g%%", *gateMin)}
	}

	last := v.Points[len(v.Points)-1]
	cur := series[len(series)-1]
	v.CurLabel = fmt.Sprintf("%.1f%%", cur.TotalPct)
	v.CurX = last.X
	v.CurY = last.Y - 9
	if v.CurY < 11 { // too close to the top edge: drop below the point
		v.CurY = last.Y + 18
	}
	return v
}
