package diffcov

import (
	"reflect"
	"testing"
)

func TestMerge(t *testing.T) {
	t.Run("disjoint files sum", func(t *testing.T) {
		backend := &Result{
			Files:        []FileCoverage{{Path: "a.go", CoveredLines: 4, TotalLines: 5, UncoveredLines: []int{3}}},
			CoveredLines: 4, TotalLines: 5,
		}
		frontend := &Result{
			Files:        []FileCoverage{{Path: "b.ts", CoveredLines: 2, TotalLines: 2}},
			CoveredLines: 2, TotalLines: 2,
		}
		m, conflicts := Merge(backend, frontend)
		if len(conflicts) != 0 {
			t.Errorf("disjoint files should not conflict: %v", conflicts)
		}
		if m.CoveredLines != 6 || m.TotalLines != 7 {
			t.Errorf("totals = %d/%d, want 6/7", m.CoveredLines, m.TotalLines)
		}
		if len(m.Files) != 2 || m.Files[0].Path != "a.go" || m.Files[1].Path != "b.ts" {
			t.Errorf("files = %+v", m.Files)
		}
	})

	t.Run("same file: a line covered by either part is covered", func(t *testing.T) {
		// Same changed file in a matrix rerun; line 3 uncovered only in
		// part A, line 4 uncovered only in part B, so the merge covers both.
		a := &Result{
			Files:        []FileCoverage{{Path: "a.go", CoveredLines: 2, TotalLines: 3, UncoveredLines: []int{3}}},
			CoveredLines: 2, TotalLines: 3,
		}
		b := &Result{
			Files:        []FileCoverage{{Path: "a.go", CoveredLines: 2, TotalLines: 3, UncoveredLines: []int{4}}},
			CoveredLines: 2, TotalLines: 3,
		}
		m, _ := Merge(a, b)
		if m.TotalLines != 3 {
			t.Errorf("total = %d, want 3 (file counted once)", m.TotalLines)
		}
		if m.CoveredLines != 3 {
			t.Errorf("covered = %d, want 3 (union of both parts)", m.CoveredLines)
		}
		if len(m.Files[0].UncoveredLines) != 0 {
			t.Errorf("uncovered = %v, want none", m.Files[0].UncoveredLines)
		}
	})

	t.Run("line uncovered by both stays uncovered", func(t *testing.T) {
		a := &Result{Files: []FileCoverage{{Path: "a.go", CoveredLines: 2, TotalLines: 3, UncoveredLines: []int{3}}}, CoveredLines: 2, TotalLines: 3}
		b := &Result{Files: []FileCoverage{{Path: "a.go", CoveredLines: 2, TotalLines: 3, UncoveredLines: []int{3}}}, CoveredLines: 2, TotalLines: 3}
		m, _ := Merge(a, b)
		if !reflect.DeepEqual(m.Files[0].UncoveredLines, []int{3}) {
			t.Errorf("uncovered = %v, want [3]", m.Files[0].UncoveredLines)
		}
		if m.CoveredLines != 2 {
			t.Errorf("covered = %d, want 2", m.CoveredLines)
		}
	})

	t.Run("parts disagree on a file: conservative, never inflates", func(t *testing.T) {
		// Two tools report a.go with different changed-line counts. The
		// optimistic intersection would call lines 4 and 5 covered even
		// though no part covered anything (a silent inflation). The merge
		// must instead union the uncovered lines and flag the conflict.
		a := &Result{
			Files:        []FileCoverage{{Path: "a.go", CoveredLines: 0, TotalLines: 5, UncoveredLines: []int{1, 2, 3, 4, 5}}},
			CoveredLines: 0, TotalLines: 5,
		}
		b := &Result{
			Files:        []FileCoverage{{Path: "a.go", CoveredLines: 0, TotalLines: 3, UncoveredLines: []int{1, 2, 3}}},
			CoveredLines: 0, TotalLines: 3,
		}
		m, conflicts := Merge(a, b)
		if m.CoveredLines != 0 {
			t.Errorf("covered = %d, want 0 (no part covered any line; must not inflate)", m.CoveredLines)
		}
		if m.TotalLines != 5 {
			t.Errorf("total = %d, want 5", m.TotalLines)
		}
		if !reflect.DeepEqual(conflicts, []string{"a.go"}) {
			t.Errorf("conflicts = %v, want [a.go] (disagreement must be reported, not silent)", conflicts)
		}
	})

	t.Run("file covered by one part drops from another's unmatched", func(t *testing.T) {
		// Backend changed and covered a.go; the frontend part changed it too
		// but its profile had no data, so it reported a.go unmatched.
		backend := &Result{
			Files:        []FileCoverage{{Path: "a.go", CoveredLines: 1, TotalLines: 1}},
			CoveredLines: 1, TotalLines: 1,
		}
		frontend := &Result{UnmatchedFiles: []string{"a.go", "only.ts"}}
		m, _ := Merge(backend, frontend)
		if !reflect.DeepEqual(m.UnmatchedFiles, []string{"only.ts"}) {
			t.Errorf("unmatched = %v, want [only.ts] (a.go is covered by backend)", m.UnmatchedFiles)
		}
	})

	t.Run("nil inputs and all-nil", func(t *testing.T) {
		r := &Result{Files: []FileCoverage{{Path: "a.go", CoveredLines: 1, TotalLines: 1}}, CoveredLines: 1, TotalLines: 1}
		if m, _ := Merge(nil, r, nil); m.CoveredLines != 1 || m.TotalLines != 1 {
			t.Errorf("merge with nils = %+v", m)
		}
		if m, _ := Merge(nil, nil); m != nil {
			t.Errorf("all-nil merge = %+v, want nil", m)
		}
	})
}
