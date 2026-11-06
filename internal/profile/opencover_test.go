package profile

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// openCoverSample is shaped like Coverlet's coverage.opencover.xml: an XML
// prolog, a <CoverageSession> root, one module listing its files by uid,
// and sequence points carrying a fileid.
const openCoverSample = `<?xml version="1.0" encoding="utf-8"?>
<CoverageSession>
  <Summary numSequencePoints="6" visitedSequencePoints="4" numBranchPoints="2" visitedBranchPoints="1" sequenceCoverage="66.67" branchCoverage="50" />
  <Modules>
    <Module hash="ABC">
      <ModulePath>MyApp.dll</ModulePath>
      <ModuleName>MyApp</ModuleName>
      <Files>
        <File uid="1" fullPath="/home/runner/work/app/app/src/MyApp/Calculator.cs" />
        <File uid="2" fullPath="/home/runner/work/app/app/src/MyApp/Util.cs" />
      </Files>
      <Classes>
        <Class>
          <FullName>MyApp.Calculator</FullName>
          <Methods>
            <Method visited="true" cyclomaticComplexity="2" sequenceCoverage="75" branchCoverage="50" isConstructor="false" isStatic="false" isGetter="false" isSetter="false">
              <Name>System.Int32 MyApp.Calculator::Add(System.Int32,System.Int32)</Name>
              <FileRef uid="1" />
              <SequencePoints>
                <SequencePoint vc="3" uspid="1" ordinal="0" sl="10" sc="9" el="10" ec="10" bec="0" bev="0" fileid="1" />
                <SequencePoint vc="3" uspid="2" ordinal="1" sl="11" sc="13" el="12" ec="40" bec="2" bev="1" fileid="1" />
                <SequencePoint vc="0" uspid="3" ordinal="2" sl="14" sc="13" el="14" ec="30" bec="0" bev="0" fileid="1" />
                <SequencePoint vc="1" uspid="4" ordinal="3" sl="16707566" sc="0" el="16707566" ec="0" bec="0" bev="0" fileid="1" />
              </SequencePoints>
              <BranchPoints>
                <BranchPoint vc="1" uspid="2" ordinal="0" path="0" offset="5" offsetend="7" sl="11" fileid="1" />
                <BranchPoint vc="0" uspid="2" ordinal="1" path="1" offset="5" offsetend="9" sl="11" fileid="1" />
              </BranchPoints>
              <MethodPoint vc="3" uspid="0" ordinal="0" offset="0" sl="10" sc="9" el="10" ec="10" bec="0" bev="0" fileid="1" />
            </Method>
          </Methods>
        </Class>
        <Class>
          <FullName>MyApp.Util</FullName>
          <Methods>
            <Method visited="false">
              <Name>System.Void MyApp.Util::Log()</Name>
              <FileRef uid="2" />
              <SequencePoints>
                <SequencePoint vc="0" uspid="5" ordinal="0" sl="5" sc="9" el="5" ec="10" fileid="2" />
              </SequencePoints>
            </Method>
          </Methods>
        </Class>
      </Classes>
    </Module>
    <Module hash="DEF" skippedDueTo="Filter">
      <ModulePath>xunit.core.dll</ModulePath>
      <ModuleName>xunit.core</ModuleName>
    </Module>
  </Modules>
</CoverageSession>
`

func TestOpenCoverParserParse(t *testing.T) {
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
			name:  "coverlet style report",
			input: openCoverSample,
			want: map[string]fileWant{
				// Lines 10, 11-12 (the second point spans two lines) at 3
				// collapse into one block; 14 is missed; the hidden point
				// and the method and branch points count for nothing.
				"/home/runner/work/app/app/src/MyApp/Calculator.cs": {covered: 3, total: 4, blocks: 2},
				"/home/runner/work/app/app/src/MyApp/Util.cs":       {covered: 0, total: 1, blocks: 1},
			},
		},
		{
			name: "windows paths, method FileRef fallback, points sharing a line sum",
			input: `<CoverageSession><Modules><Module>
  <Files><File uid="7" fullPath="C:\build\src\App\Foo.cs" /></Files>
  <Classes><Class><Methods>
    <Method><FileRef uid="7" /><SequencePoints>
      <SequencePoint vc="0" sl="3" el="3" />
      <SequencePoint vc="2" sl="3" el="3" />
    </SequencePoints></Method>
  </Methods></Class></Classes>
</Module></Modules></CoverageSession>`,
			want: map[string]fileWant{
				"C:/build/src/App/Foo.cs": {covered: 1, total: 1, blocks: 1},
			},
		},
		{
			name: "unresolvable and hidden points skipped, the rest kept",
			input: `<CoverageSession><Modules><Module>
  <Files><File uid="1" fullPath="src/Foo.cs" /><File uid="2" fullPath="" /></Files>
  <Classes><Class><Methods><Method><SequencePoints>
    <SequencePoint vc="1" sl="1" el="0" fileid="1" />
    <SequencePoint vc="1" sl="5" el="5" fileid="2" />
    <SequencePoint vc="1" sl="6" el="6" fileid="9" />
    <SequencePoint vc="1" sl="7" el="7" />
    <SequencePoint vc="1" sl="12" el="16707566" fileid="1" />
  </SequencePoints></Method></Methods></Class></Classes>
</Module></Modules></CoverageSession>`,
			want: map[string]fileWant{
				// el="0" is read as a one-line point.
				"src/Foo.cs": {covered: 1, total: 1, blocks: 1},
			},
		},
		{
			name: "file listed only by another module",
			input: `<CoverageSession><Modules>
  <Module><Files><File uid="1" fullPath="src/Shared.cs" /></Files></Module>
  <Module><Classes><Class><Methods><Method><SequencePoints>
    <SequencePoint vc="1" sl="1" el="1" fileid="1" />
  </SequencePoints></Method></Methods></Class></Classes></Module>
</Modules></CoverageSession>`,
			wantErr: "no line coverage data",
		},
		{
			name: "zero start line",
			input: `<CoverageSession><Modules><Module><Files><File uid="1" fullPath="a.cs" /></Files>
  <Classes><Class><Methods><Method><SequencePoints>
    <SequencePoint vc="1" sl="0" el="1" fileid="1" />
  </SequencePoints></Method></Methods></Class></Classes></Module></Modules></CoverageSession>`,
			wantErr: "malformed line range",
		},
		{
			name: "negative visit count",
			input: `<CoverageSession><Modules><Module><Files><File uid="1" fullPath="a.cs" /></Files>
  <Classes><Class><Methods><Method><SequencePoints>
    <SequencePoint vc="-1" sl="1" el="1" fileid="1" />
  </SequencePoints></Method></Methods></Class></Classes></Module></Modules></CoverageSession>`,
			wantErr: "negative visit count",
		},
		{
			name:    "only skipped modules",
			input:   `<CoverageSession><Modules><Module skippedDueTo="Filter"><ModuleName>x</ModuleName></Module></Modules></CoverageSession>`,
			wantErr: "no line coverage data",
		},
		{
			name:    "wrong root",
			input:   `<coverage><packages/></coverage>`,
			wantErr: "opencover:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := OpenCoverParser{}.Parse(strings.NewReader(tt.input))
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

// A report of a few points, each spanning millions of lines, must be
// refused rather than cost a map update per line.
func TestOpenCoverParserBoundsLineSpans(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<CoverageSession><Modules><Module><Files><File uid="1" fullPath="a.cs" /></Files><Classes><Class><Methods><Method><SequencePoints>`)
	for range maxOpenCoverLineMarks/maxLineNumber + 1 {
		fmt.Fprintf(&b, `<SequencePoint vc="1" sl="1" el="%d" fileid="1" />`, maxLineNumber)
	}
	b.WriteString(`</SequencePoints></Method></Methods></Class></Classes></Module></Modules></CoverageSession>`)
	if _, err := (OpenCoverParser{}).Parse(strings.NewReader(b.String())); err == nil || !strings.Contains(err.Error(), "span more than") {
		t.Fatalf("Parse() error = %v, want the line-span bound", err)
	}
}

// The CLI reads only the file list, without the per-line work of Parse.
func TestOpenCoverFilePaths(t *testing.T) {
	f, _ := Lookup("opencover")
	got, err := f.FilePaths(strings.NewReader(openCoverSample))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/home/runner/work/app/app/src/MyApp/Calculator.cs", "/home/runner/work/app/app/src/MyApp/Util.cs"}
	if !slices.Equal(got, want) {
		t.Errorf("FilePaths() = %v, want %v", got, want)
	}
	if _, err := f.FilePaths(strings.NewReader("<CoverageSession><Modules>")); err == nil {
		t.Error("FilePaths() of a truncated report succeeded")
	}
	// A format without its own lister falls back to the parser.
	cob, _ := Lookup("cobertura")
	if got, err := cob.FilePaths(strings.NewReader(coberturaSample)); err != nil || !slices.Equal(got, []string{"myapp/app.py", "myapp/util.py"}) {
		t.Errorf("cobertura FilePaths() = %v, %v", got, err)
	}
}

func TestDetectOpenCover(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"coverlet report", openCoverSample, "opencover"},
		{"opencover with namespaces", "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<CoverageSession xmlns:xsd=\"http://www.w3.org/2001/XMLSchema\" xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\">", "opencover"},
		{"minified single line", `<CoverageSession><Modules><Module><Files/></Module></Modules></CoverageSession>`, "opencover"},
		// The new root must not pull the existing XML formats over.
		{"cobertura still cobertura", coberturaSample, "cobertura"},
		{"jacoco still jacoco", jacocoSample, "jacoco"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect([]byte(tt.input)); got != tt.want {
				t.Errorf("Detect() = %q, want %q", got, tt.want)
			}
		})
	}
}
