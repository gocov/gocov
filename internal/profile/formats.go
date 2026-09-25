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
	// Clover comes from PHPUnit, from Istanbul/Jest and from OpenClover
	// for the JVM, so it counts all three families as source.
	{Name: "clover", Parser: CloverParser{}, Filename: "clover.xml",
		SourceExts: []string{".php", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".vue", ".svelte",
			".java", ".groovy", ".kt"}},
	// SimpleCov writes .resultset.json; the download drops the leading dot,
	// since the name follows the commit ("abc1234-resultset.json").
	{Name: "simplecov", Parser: SimpleCovParser{}, Filename: "resultset.json", SourceExts: []string{".rb", ".rake"}},
}

// Names lists the registered formats' names, in registry order — for
// messages that tell a user which formats there are.
func Names() []string {
	names := make([]string, len(formats))
	for i, f := range formats {
		names[i] = f.Name
	}
	return names
}

// Lookup returns the format of the given name.
func Lookup(name string) (Format, bool) {
	i := slices.IndexFunc(formats, func(f Format) bool { return f.Name == name })
	if i < 0 {
		return Format{}, false
	}
	return formats[i], true
}
