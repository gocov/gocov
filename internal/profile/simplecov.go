package profile

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// SimpleCovParser parses SimpleCov .resultset.json files, Ruby's de facto
// coverage output. Both layouts are accepted: the modern per-file object
// with a "lines" array (SimpleCov >= 0.18) and the legacy bare array.
// Array entries are hit counts indexed by line; null marks a line that is
// not executable. Branch data, when present, is ignored.
type SimpleCovParser struct{}

type simplecovSuite struct {
	Coverage map[string]json.RawMessage `json:"coverage"`
}

// Parse implements Parser. A resultset may hold several suites (RSpec,
// Cucumber, ...); they merge by summing per-line hit counts.
func (SimpleCovParser) Parse(r io.Reader) (*Profile, error) {
	var suites map[string]simplecovSuite
	if err := json.NewDecoder(r).Decode(&suites); err != nil {
		return nil, fmt.Errorf("simplecov: %w", err)
	}

	files := map[string]map[int]int{} // path -> line -> summed hits
	for suite, s := range suites {
		for name, raw := range s.Coverage {
			counts, err := simplecovLines(raw)
			if err != nil {
				return nil, fmt.Errorf("simplecov: suite %q file %s: %w", suite, name, err)
			}
			if len(counts) > maxLineNumber {
				return nil, fmt.Errorf("simplecov: too many lines in %s", name)
			}
			path := strings.TrimPrefix(strings.ReplaceAll(name, `\`, "/"), "./")
			lines := files[path]
			if lines == nil {
				lines = map[int]int{}
				files[path] = lines
			}
			for i, c := range counts {
				if c == nil {
					continue
				}
				if *c < 0 {
					return nil, fmt.Errorf("simplecov: negative hit count in %s line %d", path, i+1)
				}
				lines[i+1] += *c
			}
		}
	}

	p := &Profile{Files: make([]File, 0, len(files))}
	for path, lines := range files {
		if len(lines) == 0 {
			continue
		}
		p.Files = append(p.Files, File{Path: path, Blocks: blocksFromLineHits(lines)})
	}
	if len(p.Files) == 0 {
		return nil, errors.New("simplecov: no line coverage data found")
	}
	slices.SortFunc(p.Files, func(a, b File) int { return cmp.Compare(a.Path, b.Path) })
	return p, nil
}

// simplecovLines extracts the per-line hit array from one file entry, in
// either the modern {"lines": [...]} or the legacy bare-array layout.
func simplecovLines(raw json.RawMessage) ([]*int, error) {
	var obj struct {
		Lines []*int `json:"lines"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Lines, nil
	}
	var arr []*int
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	return nil, errors.New("unrecognized coverage entry")
}
