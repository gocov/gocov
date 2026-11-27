package profile

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// LCOVParser parses LCOV tracefiles (lcov.info) as produced by JavaScript
// and TypeScript coverage tools — Jest, Vitest, nyc/Istanbul, c8 — and by
// lcov itself. Only line records (DA:) contribute: gocov tracks line and
// statement coverage, so function (FN*) and branch (BRDA/BRF/BRH) records
// are ignored.
type LCOVParser struct{}

// Parse implements Parser. Repeated SF blocks for the same file (one per
// test suite) are merged by summing per-line hit counts, and consecutive
// lines with equal counts collapse into a single block.
func (LCOVParser) Parse(r io.Reader) (*Profile, error) {
	files := map[string]map[int]int{} // path -> line -> summed hit count
	var cur map[int]int

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		l := strings.TrimRight(sc.Text(), "\r")
		switch {
		case strings.HasPrefix(l, "SF:"):
			path := strings.TrimPrefix(strings.TrimSpace(l[3:]), "./")
			if path == "" {
				return nil, fmt.Errorf("line %d: empty SF record", lineNo)
			}
			if files[path] == nil {
				files[path] = map[int]int{}
			}
			cur = files[path]
		case strings.HasPrefix(l, "DA:"):
			if cur == nil {
				return nil, fmt.Errorf("line %d: DA record outside an SF block", lineNo)
			}
			// DA:<line>,<count>[,<checksum>]
			line, rest, ok := strings.Cut(l[3:], ",")
			if !ok {
				return nil, fmt.Errorf("line %d: malformed DA record %q", lineNo, l)
			}
			hits, _, _ := strings.Cut(rest, ",")
			ln, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || ln <= 0 || ln > maxLineNumber {
				return nil, fmt.Errorf("line %d: malformed DA line number %q", lineNo, line)
			}
			count, err := strconv.Atoi(strings.TrimSpace(hits))
			if err != nil || count < 0 {
				return nil, fmt.Errorf("line %d: malformed DA hit count %q", lineNo, hits)
			}
			cur[ln] += count
		case l == "end_of_record":
			cur = nil
		default:
			// TN:, FN*, BRDA:, LF:, LH:, VER:, blank lines — ignored.
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("lcov: no SF records found")
	}

	p := &Profile{Files: make([]File, 0, len(files))}
	for path, lines := range files {
		p.Files = append(p.Files, File{Path: path, Blocks: blocksFromLineHits(lines)})
	}
	slices.SortFunc(p.Files, func(a, b File) int { return cmp.Compare(a.Path, b.Path) })
	return p, nil
}

// fromLineHits is the tail every line-oriented parser shares: one File
// per path with any lines, sorted by path, and a format-named error when
// the report carried no line coverage at all.
func fromLineHits(format string, files map[string]map[int]int) (*Profile, error) {
	p := &Profile{Files: make([]File, 0, len(files))}
	for path, lines := range files {
		if len(lines) == 0 {
			continue
		}
		p.Files = append(p.Files, File{Path: path, Blocks: blocksFromLineHits(lines)})
	}
	if len(p.Files) == 0 {
		return nil, errors.New(format + ": no line coverage data found")
	}
	slices.SortFunc(p.Files, func(a, b File) int { return cmp.Compare(a.Path, b.Path) })
	return p, nil
}

// blocksFromLineHits converts per-line hit counts to blocks, collapsing
// only strictly consecutive lines with equal counts — a gap must never end
// up inside a block, or diff coverage would treat non-executable lines as
// executable. Shared by the line-oriented parsers (lcov, jacoco).
func blocksFromLineHits(lines map[int]int) []Block {
	nums := slices.AppendSeq(make([]int, 0, len(lines)), maps.Keys(lines))
	slices.Sort(nums)

	var blocks []Block
	for _, ln := range nums {
		count := lines[ln]
		if n := len(blocks); n > 0 {
			last := &blocks[n-1]
			if ln == last.EndLine+1 && count == last.Count {
				last.EndLine = ln
				last.NumStmts++
				continue
			}
		}
		blocks = append(blocks, Block{
			StartLine: ln, StartCol: 1,
			EndLine: ln, EndCol: 1,
			NumStmts: 1, Count: count,
		})
	}
	return blocks
}
