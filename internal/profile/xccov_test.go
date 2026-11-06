package profile

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// xccovSample is shaped like `xcrun xccov view --archive --json`: one
// line of JSON, files keyed by absolute path, an entry per source line.
const xccovSample = `{"/Users/runner/work/app/app/Sources/App/Calculator.swift":[` +
	`{"isExecutable":false,"line":1},` +
	`{"isExecutable":true,"line":3,"executionCount":5},` +
	`{"isExecutable":true,"line":4,"executionCount":5,"subranges":[{"column":12,"executionCount":0,"length":8}]},` +
	`{"isExecutable":true,"line":6,"executionCount":0},` +
	`{"isExecutable":false,"line":7}],` +
	`"/Users/runner/work/app/app/Sources/App/Empty.swift":[{"isExecutable":false,"line":1}],` +
	`"/Users/runner/work/app/app/Sources/App/Util.swift":[{"isExecutable":true,"line":2,"executionCount":0}]}`

func TestXccovParserParse(t *testing.T) {
	type fileWant struct {
		covered, total int64
		blocks         int
	}
	tests := []struct {
		name    string
		input   string
		wantErr string
		want    map[string]fileWant
	}{
		{
			name:  "archive report",
			input: xccovSample,
			want: map[string]fileWant{
				// Lines 3-4 ran 5 times and collapse (the partial subrange
				// on 4 does not count against it); 6 is missed. A file
				// with no executable line is left out.
				"/Users/runner/work/app/app/Sources/App/Calculator.swift": {covered: 2, total: 3, blocks: 2},
				"/Users/runner/work/app/app/Sources/App/Util.swift":       {covered: 0, total: 1, blocks: 1},
			},
		},
		{
			name: "pretty-printed, executable line without a count",
			input: `{
  "Sources/App/Foo.swift" : [
    { "isExecutable" : true, "line" : 1 },
    { "isExecutable" : true, "line" : 2, "executionCount" : 1 }
  ]
}`,
			want: map[string]fileWant{
				"Sources/App/Foo.swift": {covered: 1, total: 2, blocks: 2},
			},
		},
		{
			name:    "report schema refused with a pointer to --archive",
			input:   `{"coveredLines":10,"executableLines":20,"lineCoverage":0.5,"targets":[{"name":"App.app","files":[]}]}`,
			wantErr: "--archive",
		},
		{
			name:    "malformed line number",
			input:   `{"a.swift":[{"isExecutable":true,"line":0,"executionCount":1}]}`,
			wantErr: "malformed line number",
		},
		{
			name:    "negative count",
			input:   `{"a.swift":[{"isExecutable":true,"line":1,"executionCount":-1}]}`,
			wantErr: "negative execution count",
		},
		{
			name:    "file value not an array",
			input:   `{"a.swift":{"line":1}}`,
			wantErr: "file a.swift",
		},
		{
			name:    "no executable lines",
			input:   `{}`,
			wantErr: "no line coverage data",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := XccovParser{}.Parse(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(p.Files) != len(tt.want) {
				t.Fatalf("got %d files, want %d: %+v", len(p.Files), len(tt.want), p.Files)
			}
			for _, f := range p.Files {
				w, ok := tt.want[f.Path]
				if !ok {
					t.Errorf("unexpected file %q", f.Path)
					continue
				}
				c, tot := f.Coverage()
				if c != w.covered || tot != w.total || len(f.Blocks) != w.blocks {
					t.Errorf("%s: covered %d/%d in %d blocks, want %d/%d in %d", f.Path, c, tot, len(f.Blocks), w.covered, w.total, w.blocks)
				}
			}
		})
	}
}

func TestXccovFilePaths(t *testing.T) {
	f, _ := Lookup("xccov")
	got, err := f.FilePaths(strings.NewReader(xccovSample))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/Users/runner/work/app/app/Sources/App/Calculator.swift",
		"/Users/runner/work/app/app/Sources/App/Empty.swift",
		"/Users/runner/work/app/app/Sources/App/Util.swift",
	}
	if !slices.Equal(got, want) {
		t.Errorf("FilePaths() = %v, want %v", got, want)
	}
	if _, err := f.FilePaths(strings.NewReader(`{"executableLines":1,"targets":[]}`)); !errors.Is(err, errXccovReport) {
		t.Errorf("FilePaths() of the report schema: err = %v, want errXccovReport", err)
	}
	if _, err := f.FilePaths(strings.NewReader(`[1]`)); err == nil {
		t.Error("FilePaths() of a JSON array succeeded")
	}
}

func TestDetectXccov(t *testing.T) {
	// xccov writes one line, which for a real project runs past the
	// 4 MiB Detect's line scanner accepts.
	long := `{"/a/` + strings.Repeat("x", 5<<20) + `.swift":[{"isExecutable":true,"line":1,"executionCount":1}]}`
	long = `{"/a/B.swift":[{"isExecutable":true,"line":1,"executionCount":1}],` + long[1:]
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"archive report", xccovSample, "xccov"},
		{"pretty-printed", "{\n  \"/a/B.swift\" : [\n    {\n      \"isExecutable\" : false,\n      \"line\" : 1\n    }\n  ]\n}\n", "xccov"},
		{"single line past the scanner's limit", long, "xccov"},
		{"report schema", `{"coveredLines":1,"executableLines":2,"lineCoverage":0.5,"targets":[]}`, "xccov"},
		// The other JSON format must stay itself.
		{"simplecov still simplecov", `{"RSpec": {"coverage": {"/app/a.rb": {"lines": [1, null]}}, "timestamp": 1}}`, "simplecov"},
		{"other json", `{"foo": 1}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect([]byte(tt.input)); got != tt.want {
				t.Errorf("Detect() = %q, want %q", got, tt.want)
			}
		})
	}
}
