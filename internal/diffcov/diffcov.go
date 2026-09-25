// Package diffcov computes diff coverage: which changed lines of a pull
// request are covered by tests. It intersects the added lines of a unified
// diff with the per-file statement blocks of the normalized coverage model.
package diffcov

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"iter"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gocov/gocov/internal/profile"
)

// FileBlocks is the per-file coverage input, decoupled from the store.
type FileBlocks struct {
	Path   string // as reported by the coverage profile (may be module-qualified)
	Blocks []profile.Block
}

// FileCoverage is the diff coverage of one changed file.
type FileCoverage struct {
	// Path is the repo-relative path from the diff (not the profile path).
	Path           string `json:"path"`
	CoveredLines   int64  `json:"covered_lines"`
	TotalLines     int64  `json:"total_lines"`
	UncoveredLines []int  `json:"uncovered_lines,omitempty"`
}

// Result is the diff coverage of a whole pull request.
type Result struct {
	// Files lists changed files that contain executable changed lines.
	Files        []FileCoverage `json:"files"`
	CoveredLines int64          `json:"covered_lines"`
	TotalLines   int64          `json:"total_lines"`
	// UnmatchedFiles are diff paths with added lines but no coverage data.
	// Callers decide which of these matter (e.g. only source files).
	UnmatchedFiles []string `json:"unmatched_files,omitempty"`
}

// Percent returns the diff coverage percentage. A diff with no executable
// changed lines is fully covered by definition.
func (r *Result) Percent() float64 {
	if r.TotalLines == 0 {
		return 100
	}
	return float64(r.CoveredLines) / float64(r.TotalLines) * 100
}

var hunkRe = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// ParseUnifiedDiff extracts the added line numbers (new-file numbering) per
// file from a unified diff, as produced by git or the Bitbucket diff API.
// Deleted files are skipped; a/ and b/ prefixes are stripped.
//
// Hunk extents are tracked via the line counts in the @@ header, so hunk
// content that itself looks like diff syntax (e.g. a removed SQL comment
// rendered as "--- foo", or added text rendered as "+++ bar") is never
// mistaken for file headers.
func ParseUnifiedDiff(r io.Reader) (map[string][]int, error) {
	added := map[string][]int{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		file           string // current target file; "" when none (e.g. deleted file)
		line           int    // next new-file line number within the current hunk
		oldRem, newRem int    // content lines remaining in the current hunk
		lineNo         int
	)
	for sc.Scan() {
		lineNo++
		l := sc.Text()

		if oldRem > 0 || newRem > 0 {
			// Inside a hunk: every line is content, never a header.
			switch {
			case strings.HasPrefix(l, "+"):
				if file != "" {
					added[file] = append(added[file], line)
				}
				line++
				newRem--
			case strings.HasPrefix(l, "-"):
				oldRem--
			case strings.HasPrefix(l, `\`):
				// "\ No newline at end of file" — not counted
			default:
				// context line (starts with ' '; tolerate empty lines too)
				line++
				oldRem--
				newRem--
			}
			continue
		}

		switch {
		case strings.HasPrefix(l, "diff "):
			file = ""
		case strings.HasPrefix(l, "+++ "):
			file = targetPath(l)
		case strings.HasPrefix(l, "@@"):
			m := hunkRe.FindStringSubmatch(l)
			if m == nil {
				return nil, fmt.Errorf("line %d: malformed hunk header %q", lineNo, l)
			}
			start, err := strconv.Atoi(m[2])
			if err != nil {
				return nil, fmt.Errorf("line %d: malformed hunk header %q", lineNo, l)
			}
			line = start
			oldRem = hunkCount(m[1])
			newRem = hunkCount(m[3])
		default:
			// diff metadata (---, index, mode, rename, binary) — ignore.
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for _, lines := range added {
		slices.Sort(lines)
	}
	return added, nil
}

// hunkCount parses an optional hunk length; "@@ -1 +1 @@" means count 1.
func hunkCount(s string) int {
	if s == "" {
		return 1
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 1
	}
	return n
}

// targetPath extracts the new-file path from a "+++ b/..." line.
// Returns "" for /dev/null (deleted files).
func targetPath(l string) string {
	p := strings.TrimPrefix(l, "+++ ")
	// git may append "\t<timestamp>" after the path
	if i := strings.IndexByte(p, '\t'); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimSpace(p)
	// With core.quotePath (the default), git C-quotes paths containing
	// non-ASCII or special bytes: +++ "b/caf\303\251.go". Go's quoted
	// string syntax is a superset of git's, so Unquote decodes them.
	if strings.HasPrefix(p, `"`) {
		if uq, err := strconv.Unquote(p); err == nil {
			p = uq
		} else {
			p = strings.Trim(p, `"`)
		}
	}
	if p == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(p, "b/")
}

// Compute intersects added diff lines with coverage blocks. Profile paths
// are module-qualified while diff paths are repo-relative; pathPrefix (e.g.
// the Go module path) makes the mapping exact: profile path must equal
// pathPrefix+"/"+diffPath. When pathPrefix is empty a suffix heuristic is
// used instead, which requires the diff path to have at least two path
// components — a bare basename match would happily bind a changed file to
// an unrelated one.
func Compute(files []FileBlocks, added map[string][]int, pathPrefix string) *Result {
	res := &Result{}
	idx := newProfileIndex(files, pathPrefix)
	for _, dp := range slices.Sorted(maps.Keys(added)) {
		lines := added[dp]
		if len(lines) == 0 {
			continue
		}
		fb := idx.match(dp, pathPrefix)
		if fb == nil {
			res.UnmatchedFiles = append(res.UnmatchedFiles, dp)
			continue
		}

		// executable[l] exists if changed line l is inside a statement
		// block; value true if such a block ran (Statement, Ran). Lines are
		// looked up in merged block spans rather than expanding each block
		// line by line: block ranges come from the uploader and may claim
		// millions of lines.
		all := MergedSpans(fb.Blocks, Statement)
		ran := MergedSpans(fb.Blocks, Ran)
		executable := map[int]bool{}
		for _, l := range lines {
			if inSpans(all, l) {
				executable[l] = inSpans(ran, l)
			}
		}
		if len(executable) == 0 {
			continue // no executable changed lines in this file
		}

		fc := FileCoverage{Path: dp, TotalLines: int64(len(executable))}
		for l, covered := range executable {
			if covered {
				fc.CoveredLines++
			} else {
				fc.UncoveredLines = append(fc.UncoveredLines, l)
			}
		}
		slices.Sort(fc.UncoveredLines)
		res.Files = append(res.Files, fc)
		res.CoveredLines += fc.CoveredLines
		res.TotalLines += fc.TotalLines
	}
	return res
}

// Span is an inclusive run of line numbers.
type Span struct{ Start, End int }

// String renders the span as "N" for a single line or "N-M" for a range.
func (sp Span) String() string {
	if sp.Start == sp.End {
		return strconv.Itoa(sp.Start)
	}
	return fmt.Sprintf("%d-%d", sp.Start, sp.End)
}

// A line's coverage, by the one rule every view of it uses — diff
// coverage, the source view, the files card's uncovered and newly
// uncovered ranges: a line is executable when a statement block spans it,
// and covered when such a block over it ran. A block without statements
// (Go's empty bodies) is not code a test can miss, just as it carries no
// weight in the statement-based total.

// Statement reports whether a block holds statements, so spans lines that
// are executable.
func Statement(b profile.Block) bool { return b.NumStmts > 0 }

// Ran reports whether a block holds statements and ran, so covers the
// lines it spans.
func Ran(b profile.Block) bool { return b.NumStmts > 0 && b.Count > 0 }

// MissedSpans returns the executable lines no block ran over.
func MissedSpans(blocks []profile.Block) []Span {
	return SubtractSpans(MergedSpans(blocks, Statement), MergedSpans(blocks, Ran))
}

// SubtractSpans returns the lines of a not in b; both sorted and merged.
func SubtractSpans(a, b []Span) []Span {
	var out []Span
	j := 0
	for _, sp := range a {
		start := sp.Start
		for j < len(b) && b[j].End < start {
			j++
		}
		for k := j; k < len(b) && b[k].Start <= sp.End; k++ {
			if b[k].Start > start {
				out = append(out, Span{Start: start, End: b[k].Start - 1})
			}
			start = max(start, b[k].End+1)
		}
		if start <= sp.End {
			out = append(out, Span{Start: start, End: sp.End})
		}
	}
	return out
}

// IntersectSpans returns the lines in both a and b; both sorted and merged.
func IntersectSpans(a, b []Span) []Span {
	var out []Span
	for i, j := 0, 0; i < len(a) && j < len(b); {
		if start, end := max(a[i].Start, b[j].Start), min(a[i].End, b[j].End); start <= end {
			out = append(out, Span{Start: start, End: end})
		}
		if a[i].End < b[j].End {
			i++
		} else {
			j++
		}
	}
	return out
}

// MergedSpans returns the lines spanned by the blocks keep accepts, as sorted
// spans with overlapping and adjacent ones joined. Lines below 1 are dropped.
// The cost follows the number of blocks, never the lines they claim: block
// ranges come from uploaders and may declare millions of lines.
func MergedSpans(blocks []profile.Block, keep func(profile.Block) bool) []Span {
	var spans []Span
	for _, b := range blocks {
		if keep(b) && max(b.StartLine, 1) <= b.EndLine {
			spans = append(spans, Span{max(b.StartLine, 1), b.EndLine})
		}
	}
	slices.SortFunc(spans, func(a, b Span) int { return cmp.Compare(a.Start, b.Start) })
	var merged []Span
	for _, sp := range spans {
		if n := len(merged); n > 0 && sp.Start <= merged[n-1].End+1 {
			merged[n-1].End = max(merged[n-1].End, sp.End)
			continue
		}
		merged = append(merged, sp)
	}
	return merged
}

// inSpans reports whether line l falls inside any of the sorted, merged spans.
func inSpans(spans []Span, l int) bool {
	i, _ := slices.BinarySearchFunc(spans, l, func(sp Span, l int) int { return cmp.Compare(sp.End, l) })
	return i < len(spans) && spans[i].Start <= l
}

// profileIndex finds the coverage entry for a repo-relative diff path,
// built once per Compute so that pairing every changed file costs its
// path's depth rather than a scan of every profile file. With a
// pathPrefix the match is exact. Without one, two suffix directions are
// tried, bare basenames never matching either way:
//   - profile path ends with the diff path (module-qualified profiles,
//     e.g. Go: "example.com/mod/a/b.go" vs "a/b.go"); the shortest
//     profile path wins, being the least ambiguous, the first listed on
//     a tie.
//   - diff path ends with the profile path (package-qualified profiles,
//     e.g. JaCoCo: "com/example/Foo.java" vs
//     "src/main/java/com/example/Foo.java"); the longest profile path
//     wins, being the most specific.
type profileIndex struct {
	files  []FileBlocks
	byPath map[string]int // path → index, the first listed for a repeat
	// forward maps each directory-aligned suffix with a slash in it to
	// the profile path the forward rule picks for it.
	forward map[string]int
}

func newProfileIndex(files []FileBlocks, pathPrefix string) *profileIndex {
	idx := &profileIndex{files: files, byPath: make(map[string]int, len(files))}
	for i, f := range files {
		if _, seen := idx.byPath[f.Path]; !seen {
			idx.byPath[f.Path] = i
		}
	}
	if pathPrefix != "" {
		return idx // exact matching only
	}
	idx.forward = map[string]int{}
	for i, f := range files {
		for suffix := range dirSuffixes(f.Path) {
			if !strings.Contains(suffix, "/") {
				break // shorter suffixes are bare names too
			}
			if best, ok := idx.forward[suffix]; !ok || len(f.Path) < len(files[best].Path) {
				idx.forward[suffix] = i
			}
		}
	}
	return idx
}

func (idx *profileIndex) match(diffPath, pathPrefix string) *FileBlocks {
	if pathPrefix != "" {
		want := strings.TrimSuffix(pathPrefix, "/") + "/" + diffPath
		i, okWant := idx.byPath[want]
		j, okDiff := idx.byPath[diffPath]
		switch {
		case okWant && (!okDiff || i < j):
			return &idx.files[i]
		case okDiff:
			return &idx.files[j]
		}
		return nil
	}
	if i, ok := idx.byPath[diffPath]; ok {
		return &idx.files[i]
	}
	if strings.Contains(diffPath, "/") {
		if i, ok := idx.forward[diffPath]; ok {
			return &idx.files[i]
		}
	}
	for suffix := range dirSuffixes(diffPath) { // longest first
		if !strings.Contains(suffix, "/") {
			break
		}
		if i, ok := idx.byPath[suffix]; ok {
			return &idx.files[i]
		}
	}
	return nil
}

// dirSuffixes yields the directory-aligned proper suffixes of a path,
// longest first: "a/b/c.go" yields "b/c.go", then "c.go".
func dirSuffixes(p string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for i := strings.IndexByte(p, '/'); i >= 0; {
			p = p[i+1:]
			if !yield(p) {
				return
			}
			i = strings.IndexByte(p, '/')
		}
	}
}

// DiffPaths is the set of files a PR's diff touches, asked of profile
// paths under the same pairing rule Compute uses to bind them, so a file
// flagged as changed is one diff coverage would have measured.
type DiffPaths struct {
	paths map[string]bool
	// suffixes holds every directory-aligned suffix of every diff path,
	// for the reverse rule.
	suffixes map[string]bool
}

// NewDiffPaths indexes the repo-relative paths of a diff.
func NewDiffPaths(paths []string) DiffPaths {
	d := DiffPaths{paths: make(map[string]bool, len(paths)), suffixes: map[string]bool{}}
	for _, p := range paths {
		d.paths[p] = true
		for suffix := range dirSuffixes(p) {
			d.suffixes[suffix] = true
		}
	}
	return d
}

// Touches reports whether the diff touches the file at a profile path:
// exact (after the upload's path prefix) when a prefix is known,
// otherwise by a directory-aligned suffix in either direction. A bare
// file name never matches by suffix — "main.go" in the diff must not
// flag every main.go in the profile.
func (d DiffPaths) Touches(profilePath, pathPrefix string) bool {
	if d.paths[profilePath] {
		return true
	}
	if pathPrefix != "" {
		repoPath, _ := strings.CutPrefix(profilePath, strings.TrimSuffix(pathPrefix, "/")+"/")
		return d.paths[repoPath]
	}
	for suffix := range dirSuffixes(profilePath) {
		if !strings.Contains(suffix, "/") {
			break
		}
		if d.paths[suffix] {
			return true // the profile path ends with a diff path
		}
	}
	return strings.Contains(profilePath, "/") && d.suffixes[profilePath] // a diff path ends with it
}

// Clone returns a deep copy, so stored results cannot alias caller slices.
func (r *Result) Clone() *Result {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Files = make([]FileCoverage, len(r.Files))
	for i, f := range r.Files {
		cp.Files[i] = f
		cp.Files[i].UncoveredLines = append([]int(nil), f.UncoveredLines...)
	}
	cp.UnmatchedFiles = append([]string(nil), r.UnmatchedFiles...)
	return &cp
}

// LineSpans groups sorted line numbers into runs of consecutive lines:
// [45 46 47 52] is 45-47 and 52. A repeated line stays in its run.
func LineSpans(lines []int) []Span {
	if len(lines) == 0 {
		return nil
	}
	var spans []Span
	sp := Span{lines[0], lines[0]}
	for _, l := range lines[1:] {
		if l == sp.End || l == sp.End+1 {
			sp.End = l
			continue
		}
		spans = append(spans, sp)
		sp = Span{l, l}
	}
	return append(spans, sp)
}

// Ranges renders sorted line numbers as compact ranges: "45-47, 52".
func Ranges(lines []int) string {
	parts := make([]string, 0, len(lines))
	for _, sp := range LineSpans(lines) {
		parts = append(parts, sp.String())
	}
	return strings.Join(parts, ", ")
}
