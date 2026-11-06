package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// XccovParser parses the per-line JSON Xcode's coverage tool writes for a
// result bundle, `xcrun xccov view --archive --json Tests.xcresult`: an
// object keyed by each source file's path, holding one entry per line with
// its line number, whether it is executable, and — when it is — how often
// it ran. Partial-line subranges are ignored: a line counts by its own
// execution count, like the other line-oriented formats.
//
// The --report schema of the same tool carries per-target and per-function
// totals only, with no lines to diff against, so it is refused with a
// pointer to --archive rather than read.
type XccovParser struct{}

type xccovLine struct {
	Line           int  `json:"line"`
	IsExecutable   bool `json:"isExecutable"`
	ExecutionCount int  `json:"executionCount"`
}

// errXccovReport is what an upload of `xccov view --report --json` gets:
// valid xccov output, but not the schema that has lines in it.
var errXccovReport = errors.New("xccov: this is the --report output, which has no per-line data; " +
	"export with xcrun xccov view --archive --json instead")

// Parse implements Parser. Repeated entries for one line sum.
func (XccovParser) Parse(r io.Reader) (*Profile, error) {
	var report map[string]json.RawMessage
	if err := json.NewDecoder(r).Decode(&report); err != nil {
		return nil, fmt.Errorf("xccov: %w", err)
	}
	if _, ok := report["targets"]; ok {
		if _, ok := report["executableLines"]; ok {
			return nil, errXccovReport
		}
	}

	files := map[string]map[int]int{} // path -> line -> summed hits
	for name, raw := range report {
		var entries []xccovLine
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("xccov: file %s: %w", name, err)
		}
		path := normalizePath(name)
		lines := files[path]
		if lines == nil {
			lines = map[int]int{}
			files[path] = lines
		}
		for _, e := range entries {
			if !e.IsExecutable {
				continue
			}
			if e.Line <= 0 || e.Line > maxLineNumber {
				return nil, fmt.Errorf("xccov: malformed line number %d in %s", e.Line, path)
			}
			if e.ExecutionCount < 0 {
				return nil, fmt.Errorf("xccov: negative execution count in %s line %d", path, e.Line)
			}
			lines[e.Line] += e.ExecutionCount
		}
	}

	return fromLineHits("xccov", files)
}

// xccovPaths lists the files an xccov archive report names — its top-level
// keys — skipping over each file's line entries without decoding them.
func xccovPaths(r io.Reader) ([]string, error) {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("xccov: not a JSON object")
	}
	var paths []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("xccov: %w", err)
		}
		key, _ := tok.(string)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, fmt.Errorf("xccov: %w", err)
		}
		if key == "targets" || key == "executableLines" {
			return nil, errXccovReport
		}
		paths = append(paths, normalizePath(key))
	}
	return paths, nil
}

// detectXccov recognizes xccov JSON from the head of the data rather than
// line by line: xccov writes its JSON on a single line, which for a real
// project is longer than Detect's line scanner takes. The --report schema
// is claimed too, so its upload gets the parser's pointer to --archive
// instead of "unknown format".
func detectXccov(data []byte) bool {
	head := data[:min(len(data), 64*1024)]
	if t := bytes.TrimSpace(head); len(t) == 0 || t[0] != '{' {
		return false
	}
	if bytes.Contains(head, []byte(`"isExecutable"`)) {
		return true
	}
	return bytes.Contains(head, []byte(`"targets"`)) && bytes.Contains(head, []byte(`"executableLines"`))
}
