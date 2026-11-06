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

// isXccovReportKey reports whether a top-level key belongs to the --report
// schema, whose totals sit where the archive schema has file paths; no
// real source path is one of these bare names.
func isXccovReportKey(k string) bool {
	switch k {
	case "coveredLines", "executableLines", "lineCoverage", "targets":
		return true
	}
	return false
}

// walkXccov streams an archive report's top-level object, calling fn with
// each file's path while the decoder sits on that file's entries; fn must
// consume them. Streaming keeps a large single-line report from being held
// twice over, as decoding it into a map first would.
func walkXccov(r io.Reader, fn func(path string, dec *json.Decoder) error) error {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return errors.New("xccov: not a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("xccov: %w", err)
		}
		key, _ := tok.(string)
		if isXccovReportKey(key) {
			return errXccovReport
		}
		if err := fn(normalizePath(key), dec); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("xccov: %w", err)
	}
	return nil
}

// Parse implements Parser. Repeated entries for one line sum.
func (XccovParser) Parse(r io.Reader) (*Profile, error) {
	files := map[string]map[int]int{} // path -> line -> summed hits
	err := walkXccov(r, func(path string, dec *json.Decoder) error {
		// A fresh slice per file: decoding into a reused one would leave a
		// previous file's executionCount on entries that omit it.
		var entries []xccovLine
		if err := dec.Decode(&entries); err != nil {
			return fmt.Errorf("xccov: file %s: %w", path, err)
		}
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
				return fmt.Errorf("xccov: malformed line number %d in %s", e.Line, path)
			}
			if e.ExecutionCount < 0 {
				return fmt.Errorf("xccov: negative execution count in %s line %d", path, e.Line)
			}
			lines[e.Line] += e.ExecutionCount
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return fromLineHits("xccov", files)
}

// xccovPaths lists the files an xccov archive report names — its top-level
// keys — skipping over each file's line entries without decoding them.
func xccovPaths(r io.Reader) ([]string, error) {
	var paths []string
	err := walkXccov(r, func(path string, dec *json.Decoder) error {
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return fmt.Errorf("xccov: %w", err)
		}
		paths = append(paths, path)
		return nil
	})
	return paths, err
}

// detectXccov recognizes xccov JSON in the head of a report as a whole,
// not line by line: xccov writes one line, and pretty-printed its markers
// sit lines below the opening brace. The --report schema is claimed too,
// so its upload gets the parser's pointer to --archive instead of
// "unknown format"; as a substring guess it wants both of its keys.
func detectXccov(head []byte) bool {
	if t := bytes.TrimSpace(head); len(t) == 0 || t[0] != '{' {
		return false
	}
	if bytes.Contains(head, []byte(`"isExecutable"`)) {
		return true
	}
	return bytes.Contains(head, []byte(`"targets"`)) && bytes.Contains(head, []byte(`"executableLines"`))
}
