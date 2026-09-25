package profile

import "slices"

// Format is one coverage format this build understands: the name the
// upload API, the CLI and Detect use, its parser, and what the rest of
// the pipeline needs to know about its reports. Adding a format is one
// entry in formats.
type Format struct {
	Name   string
	Parser Parser
	// Filename is the conventional name of the raw report, used when it
	// is downloaded; empty falls back to "coverage.<name>".
	Filename string
	// SourceExts are the extensions of source files whose absence from a
	// report is worth flagging in diff coverage — a changed README is not
	// untested code. Empty flags every changed file.
	SourceExts []string
}

var formats = []Format{
	{Name: "go", Parser: GoParser{}, Filename: "coverage.out", SourceExts: []string{".go"}},
	{Name: "lcov", Parser: LCOVParser{}, Filename: "lcov.info",
		SourceExts: []string{".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".vue", ".svelte"}},
	{Name: "jacoco", Parser: JaCoCoParser{}, Filename: "jacoco.xml",
		SourceExts: []string{".java", ".kt", ".kts", ".scala", ".groovy"}},
	{Name: "cobertura", Parser: CoberturaParser{}, Filename: "cobertura.xml",
		SourceExts: []string{".py", ".cs", ".php", ".cpp", ".cc", ".c"}},
	{Name: "clover", Parser: CloverParser{}},
	{Name: "simplecov", Parser: SimpleCovParser{}},
}

// Lookup returns the format of the given name.
func Lookup(name string) (Format, bool) {
	i := slices.IndexFunc(formats, func(f Format) bool { return f.Name == name })
	if i < 0 {
		return Format{}, false
	}
	return formats[i], true
}
