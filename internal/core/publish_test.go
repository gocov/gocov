package core

import (
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/store"
)

func TestInsightsAnnotationsCollapseRanges(t *testing.T) {
	dc := &diffcov.Result{
		Files: []diffcov.FileCoverage{
			{Path: "a.go", UncoveredLines: []int{5, 6, 7, 10}},
			{Path: "b.go", UncoveredLines: []int{1}},
		},
		UnmatchedFiles: []string{"c.go"},
	}
	anns, dropped := insightsAnnotations(dc)
	if dropped != 0 {
		t.Fatalf("dropped = %d", dropped)
	}
	want := []forge.Annotation{
		{Path: "c.go", Summary: "This changed file has no coverage data — nothing in it appears to be tested"},
		{Path: "a.go", Line: 5, EndLine: 7, Summary: "Lines 5–7 of this change are not covered by tests"},
		{Path: "a.go", Line: 10, EndLine: 10, Summary: "Line 10 of this change is not covered by tests"},
		{Path: "b.go", Line: 1, EndLine: 1, Summary: "Line 1 of this change is not covered by tests"},
	}
	if len(anns) != len(want) {
		t.Fatalf("annotations = %+v", anns)
	}
	for i := range want {
		if anns[i] != want[i] {
			t.Errorf("annotation[%d] = %+v, want %+v", i, anns[i], want[i])
		}
	}
}

func TestInsightsAnnotationsTruncate(t *testing.T) {
	p := &Pipeline{BaseURL: "https://cov.example.com"}
	// 105 non-contiguous uncovered lines: odd numbers 1..209.
	lines := make([]int, 105)
	for i := range lines {
		lines[i] = 2*i + 1
	}
	dc := &diffcov.Result{
		Files:        []diffcov.FileCoverage{{Path: "big.go", UncoveredLines: lines}},
		TotalLines:   105,
		CoveredLines: 0,
	}
	anns, dropped := insightsAnnotations(dc)
	if len(anns) != insightsMaxAnnotations || dropped != 5 {
		t.Fatalf("got %d annotations, %d dropped; want 100/5", len(anns), dropped)
	}

	// The truncation is called out in the report details.
	report, _ := p.insightsReport(&store.Upload{
		TotalPct: 80, CoveredStmts: 8, TotalStmts: 10, DiffCoverage: dc,
	}, nil, Verdict{})
	if !strings.Contains(report.Details, "+5 more uncovered ranges") {
		t.Errorf("details = %q, want truncation note", report.Details)
	}
}

func TestInsightsPerFileDataBudget(t *testing.T) {
	p := &Pipeline{BaseURL: "https://cov.example.com"}
	// Eight partially covered files against six standard fields (delta
	// and gate present): the per-file summary must stop at the API's
	// ten-field cap, worst-covered file first.
	files := make([]diffcov.FileCoverage, 8)
	for i := range files {
		files[i] = diffcov.FileCoverage{
			Path:           string(rune('a'+i)) + ".go",
			CoveredLines:   int64(i), // i of 10 covered: a.go worst
			TotalLines:     10,
			UncoveredLines: []int{1}, // marker: has uncovered lines
		}
	}
	dc := &diffcov.Result{Files: files, CoveredLines: 28, TotalLines: 80}

	delta := 1.5
	report, _ := p.insightsReport(&store.Upload{
		TotalPct: 80, CoveredStmts: 8, TotalStmts: 10, DiffCoverage: dc,
	}, &delta, Verdict{Configured: true})

	if len(report.Data) != insightsMaxDataFields {
		t.Fatalf("data fields = %d, want %d", len(report.Data), insightsMaxDataFields)
	}
	// Six standard fields, then the four worst files a.go .. d.go.
	for i, wantTitle := range []string{"a.go", "b.go", "c.go", "d.go"} {
		d := report.Data[6+i]
		if d.Title != wantTitle || d.Type != forge.DataPercentage || d.Value != float64(i*10) {
			t.Errorf("per-file field[%d] = %+v, want %s at %d%%", i, d, wantTitle, i*10)
		}
	}
}

func TestInsightsFullyCoveredFilesClaimNoDataFields(t *testing.T) {
	p := &Pipeline{BaseURL: "https://cov.example.com"}
	dc := &diffcov.Result{
		Files: []diffcov.FileCoverage{
			{Path: "ok.go", CoveredLines: 5, TotalLines: 5},
			{Path: "bad.go", CoveredLines: 0, TotalLines: 2, UncoveredLines: []int{3, 4}},
		},
		CoveredLines: 5, TotalLines: 7,
	}
	report, _ := p.insightsReport(&store.Upload{
		TotalPct: 80, CoveredStmts: 8, TotalStmts: 10, DiffCoverage: dc,
	}, nil, Verdict{})
	for _, d := range report.Data {
		if d.Title == "ok.go" {
			t.Errorf("fully covered file claimed a data field: %+v", d)
		}
	}
	last := report.Data[len(report.Data)-1]
	if last.Title != "bad.go" || last.Value != 0.0 {
		t.Errorf("per-file field = %+v, want bad.go at 0%%", last)
	}
}

// seedRepoUpload registers a repo and one upload with a single file, so the
// repo, upload and source pages all have something to render.
