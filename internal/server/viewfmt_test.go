package server

import (
	"testing"
)

func TestHumanBytes(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1 << 10, "1 KB"},
		{1536, "2 KB"}, // rounds to whole KB
		{(1 << 20) - 1, "1024 KB"},
		{1 << 20, "1.0 MB"},
		{3*(1<<20) + (1 << 19), "3.5 MB"},
	} {
		if got := humanBytes(tc.n); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
