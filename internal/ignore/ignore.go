// Package ignore matches coverage-report paths against the glob patterns a
// repo (or one upload) asks to leave out of its coverage: generated code,
// mocks, a dev harness under cmd/. It is pure string matching — what the
// patterns are applied to, and where they come from, is the caller's.
//
// Patterns are slash-separated globs in the gitignore spirit:
//
//   - `*` and `?` match within one path segment, as in path.Match.
//   - `**` as a whole segment matches any number of segments, including
//     none. Inside a segment (`**.go`) it is read as `**/*.go`, which is
//     what people mean by it.
//   - A pattern matches at any directory level: `cmd/preview/**` ignores
//     `cmd/preview/main.go` and `tools/cmd/preview/main.go` alike, and a
//     bare name (`*_mock.go`) matches anywhere. Reports often carry paths
//     under a root the pattern never sees — a Go module path, a CI checkout
//     directory, an absolute path — and floating is what makes a pattern
//     written against the repo tree still land.
//   - A leading `/` anchors the pattern at the start of the report path
//     (or of the path with the upload's prefix removed): `/cmd/preview`
//     matches `cmd/preview/main.go` but not `tools/cmd/preview/main.go`.
//   - A pattern that matches a directory matches everything under it:
//     `cmd/preview` and `cmd/preview/**` ignore the same files.
//
// A pattern from an upload request is untrusted input applied to every
// file of the report, and matching one pattern against one path costs up
// to the product of their lengths (a `*` before a long literal is tried
// at every offset of every segment), so the counts and lengths below do
// not bound the work on their own. A caller matching untrusted input
// sets a work budget with Limit; matching past it stops and Err reports
// ErrTooCostly.
package ignore

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"sync/atomic"
)

// ErrTooCostly is what Err reports once matching has used up the budget
// set with Limit: the patterns and the paths they were matched against
// were too expensive to compare, and the matches after that point were
// not computed.
var ErrTooCostly = errors.New("ignore patterns are too costly to match against this report's file paths; use fewer or simpler patterns")

// Limits on what a settings form or an upload may submit. Patterns are
// matched against every file of every upload, and anyone holding an
// upload token can send them, so both are bounded. Validate enforces both
// for one source; Compile only the length, because a repo's patterns and
// an upload's are compiled together.
const (
	MaxPatterns      = 100
	MaxPatternLength = 200
)

// Rules is a compiled, validated pattern list.
type Rules struct {
	patterns [][]string // each pattern split into segments

	// budget is the work left, in segMatch cost units, when Limit set
	// one; nil means unlimited. Shared by every Match on these rules,
	// from any goroutine.
	budget *atomic.Int64
}

// Limit caps the total matching work every later Match on r may do, in
// cost units of roughly one byte comparison each. Once it is spent, Match
// reports false without comparing and Err returns ErrTooCostly.
func (r *Rules) Limit(units int64) {
	b := new(atomic.Int64)
	b.Store(units)
	r.budget = b
}

// Err reports ErrTooCostly once the budget set with Limit has run out,
// and nil otherwise. Matches made after that point are not to be trusted.
func (r *Rules) Err() error {
	if r != nil && r.budget != nil && r.budget.Load() < 0 {
		return ErrTooCostly
	}
	return nil
}

// Parse splits free-form input — one pattern per line, or a comma-separated
// list — into patterns, dropping blanks and `#` comment lines.
func Parse(text string) []string {
	var out []string
	for line := range strings.FieldsFuncSeq(text, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' }) {
		p := strings.TrimSpace(line)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Validate checks one source's pattern list — count and syntax — and
// returns the first problem as an error fit to show the user.
func Validate(patterns []string) error {
	if len(patterns) > MaxPatterns {
		return fmt.Errorf("too many ignore patterns: %d (the maximum is %d)", len(patterns), MaxPatterns)
	}
	_, err := Compile(patterns)
	return err
}

// Compile validates the patterns and returns the rules that match them.
// A nil or empty list compiles to rules that match nothing.
func Compile(patterns []string) (*Rules, error) {
	r := &Rules{patterns: make([][]string, 0, len(patterns))}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) > MaxPatternLength {
			return nil, fmt.Errorf("ignore pattern %.20q… is longer than %d characters", p, MaxPatternLength)
		}
		anchored := strings.HasPrefix(p, "/")
		// A trailing slash names a directory, which matches by prefix
		// anyway; accepted so a line copied from a .gitignore works.
		p = strings.Trim(p, "/")
		if p == "" {
			continue
		}
		var segs []string
		if !anchored {
			segs = append(segs, "**")
		}
		for seg := range strings.SplitSeq(p, "/") {
			if seg == "" {
				return nil, fmt.Errorf("ignore pattern %q has an empty path segment", p)
			}
			if strings.Contains(seg, "**") && seg != "**" {
				// `**.go` means "any depth, then *.go"; path.Match would
				// read the doubled star as one and stop at a single level.
				segs = append(segs, "**")
				seg = strings.ReplaceAll(seg, "**", "*")
			}
			if seg == "**" {
				// Consecutive `**` add nothing; one is enough.
				if segs[len(segs)-1] != "**" {
					segs = append(segs, seg)
				}
				continue
			}
			if _, err := path.Match(seg, ""); err != nil {
				return nil, fmt.Errorf("ignore pattern %q: %v", p, err)
			}
			segs = append(segs, seg)
		}
		r.patterns = append(r.patterns, segs)
	}
	return r, nil
}

// Match reports whether p, or any directory above it, matches a pattern.
// prefix is the upload's path prefix, or empty: when p starts with it,
// the remainder is tried as well, which is what lets an anchored pattern
// written against the repo tree match a module-qualified Go path. A nil
// *Rules matches nothing.
func (r *Rules) Match(p, prefix string) bool {
	if r == nil || len(r.patterns) == 0 {
		return false
	}
	if r.match(p) {
		return true
	}
	if prefix != "" {
		if rest, ok := strings.CutPrefix(p, strings.TrimSuffix(prefix, "/")+"/"); ok {
			return r.match(rest)
		}
	}
	return false
}

func (r *Rules) match(p string) bool {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	for _, pat := range r.patterns {
		if matchSegments(pat, segs, r.budget) {
			return true
		}
	}
	return false
}

// matchSegments matches a pattern's segments against a path's, with `**`
// standing for any run of path segments. A pattern that runs out while
// path segments remain has matched a directory, and so matches the file
// under it.
//
// It is the classic single-backtrack wildcard loop: on a mismatch, the
// most recent `**` absorbs one more segment and matching resumes after it.
// Revisiting earlier `**`s can never help, so the number of segment
// comparisons is bounded by len(pat)·len(segs) whatever the pattern looks
// like; each comparison is charged to budget (nil: unlimited), and a
// spent budget ends the match as a mismatch.
func matchSegments(pat, segs []string, budget *atomic.Int64) bool {
	pi, si := 0, 0
	starPi, starSi := -1, 0
	for si < len(segs) {
		switch {
		case pi == len(pat):
			return true // a matched directory covers everything under it
		case pat[pi] == "**":
			starPi, starSi = pi, si
			pi++
		case !charge(budget, pat[pi], segs[si]):
			return false
		case segMatch(pat[pi], segs[si]):
			pi++
			si++
		case starPi >= 0:
			starSi++
			pi, si = starPi+1, starSi
		default:
			return false
		}
	}
	// The path is used up; whatever pattern is left must be able to
	// match nothing, which only `**` can.
	for pi < len(pat) && pat[pi] == "**" {
		pi++
	}
	return pi == len(pat)
}

// charge takes the cost of comparing pattern segment pat with path
// segment seg from budget, reporting false when the budget is spent. A
// wildcard segment costs up to len(pat)·len(seg) in path.Match, a literal
// one about len(seg); the constant is the call's own overhead.
func charge(budget *atomic.Int64, pat, seg string) bool {
	if budget == nil {
		return true
	}
	cost := int64(len(seg)) + 1
	if strings.ContainsAny(pat, "*?[\\") {
		cost *= int64(len(pat)) + 1
	}
	return budget.Add(-(cost + 8)) >= 0
}

// segMatch matches one pattern segment against one path segment. The
// patterns were validated at Compile, so the error cannot happen.
func segMatch(pat, seg string) bool {
	ok, _ := path.Match(pat, seg)
	return ok
}
