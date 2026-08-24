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

func TestHumanInt(t *testing.T) {
	// Statement counts run into the millions on a large repo, and the
	// separator has to land in the same places on both sides of zero.
	for _, tc := range []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{7, "7"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{1234567, "1,234,567"},
		{-1000, "-1,000"},
		{-999, "-999"},
	} {
		if got := humanInt(tc.n); got != tc.want {
			t.Errorf("humanInt(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestNewDeltaView(t *testing.T) {
	// The class picks the colour and the arrow, and a movement too small to
	// show at one decimal reads as flat rather than as a signed "+0.0%".
	for _, tc := range []struct {
		d         float64
		wantClass string
		wantArrow string
		wantText  string
	}{
		{2.5, "up", "\u25b2", "+2.5%"},
		{-2.5, "down", "\u25bc", "-2.5%"},
		{0, "flat", "\u2014", "0.0%"},
		{0.04, "flat", "\u2014", "0.0%"},
		{-0.04, "flat", "\u2014", "0.0%"},
		{0.05, "up", "\u25b2", "+0.1%"},
	} {
		got := newDeltaView(tc.d)
		if got == nil {
			t.Fatalf("newDeltaView(%v) = nil", tc.d)
		}
		if got.Class != tc.wantClass || got.Arrow != tc.wantArrow || got.Text != tc.wantText {
			t.Errorf("newDeltaView(%v) = %+v, want class %q arrow %q text %q",
				tc.d, *got, tc.wantClass, tc.wantArrow, tc.wantText)
		}
	}
}
