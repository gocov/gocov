package core

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultPart is the part an upload that names none belongs to: a commit
// uploaded in one piece is a single-part commit under this name.
const DefaultPart = "default"

// partRe bounds a normalized part name — a canonical lowercase slug that
// starts alphanumeric. It becomes a storage key, so the charset is
// conservative and the length is bounded.
var partRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// NormalizePart turns the part name an upload carries into the one it is
// stored under. A part names one slice of the commit's coverage (backend,
// frontend, e2e, ...) uploaded from a separate CI job; it is trimmed and
// lowercased before validation, so "Backend" and " backend " key the same
// bucket whichever client sent them — the API is called directly, not only
// through the CLI. A blank name is DefaultPart.
func NormalizePart(raw string) (string, error) {
	part := strings.ToLower(strings.TrimSpace(raw))
	if part == "" {
		return DefaultPart, nil
	}
	if !partRe.MatchString(part) {
		return "", fmt.Errorf("invalid part %q: want up to 64 alphanumeric, dot, dash or underscore characters starting with a letter or digit", part)
	}
	return part, nil
}
