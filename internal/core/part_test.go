package core

import (
	"strings"
	"testing"
)

// One logical part keys one bucket whoever sends it, a blank part is the
// default, and a name that cannot be a storage key is refused.
func TestNormalizePart(t *testing.T) {
	for raw, want := range map[string]string{
		"":             DefaultPart,
		"   ":          DefaultPart,
		"backend":      "backend",
		" Backend ":    "backend",
		"e2e.chrome_1": "e2e.chrome_1",
	} {
		if got, err := NormalizePart(raw); err != nil || got != want {
			t.Errorf("NormalizePart(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"-lead", ".hidden", "has space", "a/b", strings.Repeat("a", 65)} {
		if got, err := NormalizePart(raw); err == nil {
			t.Errorf("NormalizePart(%q) = %q, want an error", raw, got)
		}
	}
}
