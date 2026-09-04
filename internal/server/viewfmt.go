// The small view helpers shared by more than one page: a coverage delta
// with the sign and colour a template needs, and the number formatting
// registered as template functions in server.go.

package server

import (
	"fmt"
	"strconv"
	"strings"
)

// deltaView is a precomputed coverage delta for the templates.
type deltaView struct {
	Class string // up, down, flat
	Arrow string
	Text  string
}

func newDeltaView(d float64) *deltaView {
	switch {
	case d >= 0.05:
		return &deltaView{"up", "▲", fmt.Sprintf("%+.1f%%", d)}
	case d <= -0.05:
		return &deltaView{"down", "▼", fmt.Sprintf("%+.1f%%", d)}
	default:
		return &deltaView{"flat", "—", "0.0%"}
	}
}

// humanBytes formats a byte count as a compact size.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// humanInt formats a statement count with thousands separators.
func humanInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
