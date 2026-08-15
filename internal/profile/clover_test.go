package profile

import (
	"strings"
	"testing"
)

const cloverPHPUnitSample = `<?xml version="1.0" encoding="UTF-8"?>
<coverage generated="1700000000">
  <project timestamp="1700000000">
    <file name="/app/src/Greeter.php">
      <class name="Greeter" namespace="App"/>
      <line num="8" type="method" name="greet" visibility="public" complexity="1" crap="1" count="3"/>
      <line num="10" type="stmt" count="3"/>
      <line num="11" type="stmt" count="3"/>
      <line num="14" type="stmt" count="0"/>
      <metrics loc="20" ncloc="18" statements="3" coveredstatements="2"/>
    </file>
    <package name="App\Util">
      <file name="/app/src/Util/Slug.php">
        <line num="5" type="stmt" count="1"/>
        <metrics statements="1" coveredstatements="1"/>
      </file>
    </package>
    <metrics files="2" statements="4" coveredstatements="3"/>
  </project>
</coverage>
`

const cloverIstanbulSample = `<?xml version="1.0" encoding="UTF-8"?>
<coverage generated="1700000000" clover="3.2.0">
  <project timestamp="1700000000" name="All files">
    <metrics statements="2" coveredstatements="1"/>
    <file name="index.js" path="/repo/src/index.js">
      <metrics statements="2" coveredstatements="1"/>
      <line num="1" count="1" type="stmt"/>
      <line num="3" count="0" type="cond" truecount="0" falsecount="1"/>
    </file>
  </project>
</coverage>
`

func TestCloverParserParse(t *testing.T) {
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
			name:  "phpunit style report",
			input: cloverPHPUnitSample,
			want: map[string]struct {
				covered, total int64
				blocks         int
			}{
				// method line 8 ignored; 10-11 (3) collapse, 14 (0) -> 2
				// blocks. Covered 2 of 3.
				"/app/src/Greeter.php":   {covered: 2, total: 3, blocks: 2},
				"/app/src/Util/Slug.php": {covered: 1, total: 1, blocks: 1},
			},
		},
		{
			name:  "istanbul style report prefers path attribute",
			input: cloverIstanbulSample,
			want: map[string]struct {
				covered, total int64
				blocks         int
			}{
				"/repo/src/index.js": {covered: 1, total: 2, blocks: 2},
			},
		},
		{
			name: "files sharing a path merge",
			input: `<coverage><project>
  <file name="src/a.php"><line num="1" type="stmt" count="1"/></file>
  <file name="src/a.php"><line num="1" type="stmt" count="2"/><line num="2" type="stmt" count="0"/></file>
</project></coverage>`,
			want: map[string]struct {
				covered, total int64
				blocks         int
			}{
				"src/a.php": {covered: 1, total: 2, blocks: 2},
			},
		},
		{
			name: "windows backslash paths normalized",
			input: `<coverage><project>
  <file name="src\app\a.php"><line num="1" type="stmt" count="1"/></file>
</project></coverage>`,
			want: map[string]struct {
				covered, total int64
				blocks         int
			}{
				"src/app/a.php": {covered: 1, total: 1, blocks: 1},
			},
		},
		{name: "empty input", input: "", wantErr: true},
		{name: "not xml", input: "hello", wantErr: true},
		{name: "no line data", input: `<coverage><project><file name="a.php"/></project></coverage>`, wantErr: true},
		{name: "only method lines", input: `<coverage><project><file name="a.php"><line num="3" type="method" count="1"/></file></project></coverage>`, wantErr: true},
		{name: "malformed line number", input: `<coverage><project><file name="a.php"><line num="0" type="stmt" count="1"/></file></project></coverage>`, wantErr: true},
		{name: "negative hit count", input: `<coverage><project><file name="a.php"><line num="1" type="stmt" count="-1"/></file></project></coverage>`, wantErr: true},
		{name: "file without name", input: `<coverage><project><file><line num="1" type="stmt" count="1"/></file></project></coverage>`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := CloverParser{}.Parse(strings.NewReader(tt.input))
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

func TestDetectClover(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"istanbul clover attribute", cloverIstanbulSample, "clover"},
		{"phpunit without clover attribute", cloverPHPUnitSample, "clover"},
		{"minified single line", `<coverage generated="1"><project><file name="a.php"/></project></coverage>`, "clover"},
		// The ambiguous <coverage> root must not swallow cobertura.
		{"cobertura doctype still cobertura", coberturaSample, "cobertura"},
		{"cobertura packages still cobertura", "<?xml version=\"1.0\"?>\n<coverage>\n<packages/>\n</coverage>", "cobertura"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect([]byte(tt.input)); got != tt.want {
				t.Errorf("Detect() = %q, want %q", got, tt.want)
			}
		})
	}
}
