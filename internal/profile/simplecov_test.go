package profile

import (
	"strings"
	"testing"
)

const simplecovModernSample = `{
  "RSpec": {
    "coverage": {
      "/repo/lib/greeter.rb": {
        "lines": [1, 5, 5, null, 0, null],
        "branches": {"[:if, 0, 3, 4, 3, 24]": {"[:then, 1, 3, 4, 3, 10]": 5}}
      },
      "/repo/lib/util.rb": {
        "lines": [null, 0]
      }
    },
    "timestamp": 1700000000
  }
}
`

const simplecovLegacySample = `{
  "MiniTest": {
    "coverage": {
      "/repo/lib/a.rb": [1, null, 0]
    },
    "timestamp": 1700000000
  }
}
`

func TestSimpleCovParserParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		want    map[string]struct {
			covered, total int64
			blocks         int
		}
	}{
		{
			name:  "modern lines object",
			input: simplecovModernSample,
			want: map[string]struct {
				covered, total int64
				blocks         int
			}{
				// lines 1 (1), 2-3 (5) collapse, 5 (0); nulls skipped ->
				// 3 blocks, covered 3 of 4.
				"/repo/lib/greeter.rb": {covered: 3, total: 4, blocks: 3},
				"/repo/lib/util.rb":    {covered: 0, total: 1, blocks: 1},
			},
		},
		{
			name:  "legacy bare array",
			input: simplecovLegacySample,
			want: map[string]struct {
				covered, total int64
				blocks         int
			}{
				"/repo/lib/a.rb": {covered: 1, total: 2, blocks: 2},
			},
		},
		{
			name: "suites merge by summing",
			input: `{
  "RSpec":    {"coverage": {"/repo/lib/a.rb": {"lines": [1, 0]}}},
  "Cucumber": {"coverage": {"/repo/lib/a.rb": {"lines": [0, 2]}}}
}`,
			want: map[string]struct {
				covered, total int64
				blocks         int
			}{
				"/repo/lib/a.rb": {covered: 2, total: 2, blocks: 2},
			},
		},
		{name: "empty input", input: "", wantErr: true},
		{name: "not json", input: "hello", wantErr: true},
		{name: "empty object", input: "{}", wantErr: true},
		{name: "no executable lines", input: `{"RSpec": {"coverage": {"/a.rb": {"lines": [null, null]}}}}`, wantErr: true},
		{name: "negative hit count", input: `{"RSpec": {"coverage": {"/a.rb": {"lines": [-1]}}}}`, wantErr: true},
		{name: "unrecognized entry", input: `{"RSpec": {"coverage": {"/a.rb": "nope"}}}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := SimpleCovParser{}.Parse(strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse() error = nil, want error; got %+v", p)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
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
				covered, total := f.Coverage()
				if covered != w.covered || total != w.total || len(f.Blocks) != w.blocks {
					t.Errorf("%s: covered/total/blocks = %d/%d/%d, want %d/%d/%d",
						f.Path, covered, total, len(f.Blocks), w.covered, w.total, w.blocks)
				}
			}
		})
	}
}

func TestDetectSimpleCov(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"pretty printed resultset", simplecovModernSample, "simplecov"},
		{"minified resultset", `{"RSpec":{"coverage":{"/a.rb":{"lines":[1,null]}},"timestamp":1}}`, "simplecov"},
		{"unrelated json", `{"foo": 1}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect([]byte(tt.input)); got != tt.want {
				t.Errorf("Detect() = %q, want %q", got, tt.want)
			}
		})
	}
}
