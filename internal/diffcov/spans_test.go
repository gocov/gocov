package diffcov

import (
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/gocov/gocov/internal/profile"
)

func TestSubtractSpans(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b []Span
		want []Span
	}{
		{"nothing from nothing", nil, nil, nil},
		{"nothing taken away", []Span{{1, 5}}, nil, []Span{{1, 5}}},
		{"all taken away", []Span{{1, 5}}, []Span{{1, 5}}, nil},
		{"a hole in the middle", []Span{{1, 10}}, []Span{{4, 6}}, []Span{{1, 3}, {7, 10}}},
		{"both ends trimmed", []Span{{3, 8}}, []Span{{1, 3}, {8, 9}}, []Span{{4, 7}}},
		{"b wider than a", []Span{{4, 6}}, []Span{{1, 10}}, nil},
		{"one line left either side", []Span{{1, 3}}, []Span{{2, 2}}, []Span{{1, 1}, {3, 3}}},
		{"b entirely before and after", []Span{{5, 6}}, []Span{{1, 2}, {9, 9}}, []Span{{5, 6}}},
		// One span of b reaching across two of a takes from both.
		{"b straddles a gap", []Span{{1, 4}, {7, 10}}, []Span{{3, 8}}, []Span{{1, 2}, {9, 10}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SubtractSpans(tc.a, tc.b); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("SubtractSpans(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestIntersectSpans(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b []Span
		want []Span
	}{
		{"either side empty", []Span{{1, 5}}, nil, nil},
		{"identical", []Span{{1, 5}}, []Span{{1, 5}}, []Span{{1, 5}}},
		{"disjoint", []Span{{1, 3}}, []Span{{5, 7}}, nil},
		{"touching is not overlapping", []Span{{1, 3}}, []Span{{4, 7}}, nil},
		{"one line in common", []Span{{1, 3}}, []Span{{3, 7}}, []Span{{3, 3}}},
		{"one inside the other", []Span{{1, 10}}, []Span{{4, 6}}, []Span{{4, 6}}},
		// The order of a's and b's spans differs: each must advance alone.
		{"many against one", []Span{{1, 2}, {4, 5}, {8, 9}}, []Span{{2, 8}}, []Span{{2, 2}, {4, 5}, {8, 8}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IntersectSpans(tc.a, tc.b); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("IntersectSpans(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			if got := IntersectSpans(tc.b, tc.a); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("IntersectSpans(%v, %v) = %v, want %v (swapped)", tc.b, tc.a, got, tc.want)
			}
		})
	}
}

func TestMissedSpans(t *testing.T) {
	blocks := []profile.Block{
		{StartLine: 1, EndLine: 4, NumStmts: 2, Count: 1},
		{StartLine: 3, EndLine: 8, NumStmts: 3, Count: 0},   // 3-4 ran in the block above
		{StartLine: 10, EndLine: 10, NumStmts: 0, Count: 0}, // no statements: not code
		{StartLine: 12, EndLine: 13, NumStmts: 1, Count: 0},
	}
	want := []Span{{5, 8}, {12, 13}}
	if got := MissedSpans(blocks); !reflect.DeepEqual(got, want) {
		t.Errorf("MissedSpans = %v, want %v", got, want)
	}
	if got := MissedSpans(nil); got != nil {
		t.Errorf("MissedSpans(nil) = %v, want nil", got)
	}
}

// The span arithmetic must agree with plain sets of lines, which is what
// the spans stand for. Short random spans over a few dozen lines make
// every adjacency and containment common.
func TestSpanArithmeticMatchesLineSets(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	const lines = 40
	randomSpans := func() []Span {
		blocks := make([]profile.Block, rng.IntN(6))
		for i := range blocks {
			start := 1 + rng.IntN(lines)
			blocks[i] = profile.Block{StartLine: start, EndLine: start + rng.IntN(5), NumStmts: 1}
		}
		return MergedSpans(blocks, Statement)
	}
	set := func(spans []Span) map[int]bool {
		s := map[int]bool{}
		for _, sp := range spans {
			for l := sp.Start; l <= sp.End; l++ {
				s[l] = true
			}
		}
		return s
	}
	for round := range 5000 {
		a, b := randomSpans(), randomSpans()
		inA, inB := set(a), set(b)
		minus, both := map[int]bool{}, map[int]bool{}
		for l := range inA {
			if inB[l] {
				both[l] = true
			} else {
				minus[l] = true
			}
		}
		if got := SubtractSpans(a, b); !reflect.DeepEqual(set(got), minus) || !sortedAndApart(got) {
			t.Fatalf("round %d: SubtractSpans(%v, %v) = %v", round, a, b, got)
		}
		if got := IntersectSpans(a, b); !reflect.DeepEqual(set(got), both) || !sortedAndApart(got) {
			t.Fatalf("round %d: IntersectSpans(%v, %v) = %v", round, a, b, got)
		}
	}
}

// sortedAndApart reports whether spans are well formed, in order and not
// overlapping, the shape the callers rely on for binary search.
func sortedAndApart(spans []Span) bool {
	for i, sp := range spans {
		if sp.Start > sp.End || i > 0 && sp.Start <= spans[i-1].End {
			return false
		}
	}
	return true
}
