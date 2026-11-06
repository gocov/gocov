package profile

import (
	"io"
	"slices"
	"strings"
)

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
	// TestSuffixes end the names of test files: source by extension, but
	// never measured themselves, so their absence from a report is no
	// finding either.
	TestSuffixes []string
	// Paths, when set, lists the files a report names more cheaply than a
	// full parse; FilePaths falls back to the parser without it.
	Paths func(r io.Reader) ([]string, error)
	// AbsolutePaths marks formats that record each file by its absolute
	// path on the machine the tests ran on, for which the CLI looks for
	// the checkout directory to send as the path prefix.
	AbsolutePaths bool
}

// FilePaths lists the source files a report of this format names.
func (f Format) FilePaths(r io.Reader) ([]string, error) {
	if f.Paths != nil {
		return f.Paths(r)
	}
	p, err := f.Parser.Parse(r)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(p.Files))
	for i, file := range p.Files {
		paths[i] = file.Path
	}
	return paths, nil
}

// jsExts are the JavaScript family's source extensions, whose test files
// Jest, Vitest and Mocha name *.test.* or *.spec.*.
var jsExts = []string{".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".vue", ".svelte"}

func jsTestSuffixes() []string {
	var out []string
	for _, ext := range jsExts {
		out = append(out, ".test"+ext, ".spec"+ext)
	}
	return out
}

// languageFamilies groups source extensions by the language — or the set
// of languages one build compiles together, like Swift with Objective-C —
// that a single test run measures. Every source extension of every format
// is in exactly one family.
var languageFamilies = [][]string{
	{".go"},
	jsExts,
	{".java", ".kt", ".kts", ".scala", ".groovy"},
	{".py"},
	{".cs", ".vb", ".fs"},
	{".php"},
	{".rb", ".rake"},
	{".rs"},
	{".dart"},
	{".ex", ".exs"},
	{".c", ".cc", ".cpp", ".m", ".mm", ".swift"},
}

// familyOf returns the index of path's language family, or -1.
func familyOf(path string) int {
	return slices.IndexFunc(languageFamilies, func(exts []string) bool {
		return slices.ContainsFunc(exts, func(ext string) bool { return strings.HasSuffix(path, ext) })
	})
}

// UnmeasuredSource returns the test diff coverage applies to a changed
// file that report p has no data for: does its absence mean untested
// code? It does when the file is one of the format's sources, is not a
// test file, and belongs to a language family p measures. That last part
// keeps formats several languages write apart: a frontend's LCOV says
// nothing about the Rust files a separate LCOV upload covers, so a changed
// .rs file is no finding against it. A format without source extensions
// counts every changed non-test file.
func (f Format) UnmeasuredSource(p *Profile) func(path string) bool {
	measured := map[int]bool{}
	for _, file := range p.Files {
		measured[familyOf(file.Path)] = true
	}
	return func(path string) bool {
		if f.IsTestFile(path) {
			return false
		}
		if len(f.SourceExts) == 0 {
			return true
		}
		if !slices.ContainsFunc(f.SourceExts, func(ext string) bool { return strings.HasSuffix(path, ext) }) {
			return false
		}
		return measured[familyOf(path)]
	}
}

// IsTestFile reports whether path names a test file of this format's
// languages.
func (f Format) IsTestFile(path string) bool {
	return slices.ContainsFunc(f.TestSuffixes, func(s string) bool { return strings.HasSuffix(path, s) })
}

var formats = []Format{
	{Name: "go", Parser: GoParser{}, Filename: "coverage.out", SourceExts: []string{".go"},
		TestSuffixes: []string{"_test.go"}},
	// LCOV is JavaScript's format, and also what cargo-llvm-cov (Rust),
	// flutter test (Dart), lcov_ex (Elixir) and lcov/gcov (C, C++) write.
	{Name: "lcov", Parser: LCOVParser{}, Filename: "lcov.info",
		SourceExts:   slices.Concat(jsExts, []string{".rs", ".dart", ".ex", ".exs", ".c", ".cc", ".cpp", ".swift", ".m", ".mm"}),
		TestSuffixes: append(jsTestSuffixes(), "_test.dart", "_test.exs")},
	{Name: "jacoco", Parser: JaCoCoParser{}, Filename: "jacoco.xml",
		SourceExts: []string{".java", ".kt", ".kts", ".scala", ".groovy"}},
	{Name: "cobertura", Parser: CoberturaParser{}, Filename: "cobertura.xml",
		SourceExts: []string{".py", ".cs", ".php", ".cpp", ".cc", ".c", ".rs", ".scala", ".swift", ".m", ".mm"}},
	// Clover comes from PHPUnit, from Istanbul/Jest and from OpenClover
	// for the JVM, so it counts all three families as source.
	{Name: "clover", Parser: CloverParser{}, Filename: "clover.xml",
		SourceExts:   slices.Concat([]string{".php"}, jsExts, []string{".java", ".groovy", ".kt"}),
		TestSuffixes: jsTestSuffixes()},
	// SimpleCov writes .resultset.json; the download drops the leading dot,
	// since the name follows the commit ("abc1234-resultset.json").
	{Name: "simplecov", Parser: SimpleCovParser{}, Filename: "resultset.json", SourceExts: []string{".rb", ".rake"},
		TestSuffixes: []string{"_spec.rb", "_test.rb"}},
	// .NET test projects live apart from the code they test and follow no
	// file-suffix convention, so OpenCover names no test files.
	{Name: "opencover", Parser: OpenCoverParser{}, Filename: "coverage.opencover.xml",
		SourceExts: []string{".cs", ".vb", ".fs"}, Paths: openCoverPaths, AbsolutePaths: true},
	// XCTest cases live in files named after the type they test plus
	// "Tests" (Xcode's template), Quick specs plus "Spec". Headers are not
	// source here: they rarely hold an executable line, so xccov seldom
	// lists them, and every changed one would read as unmeasured code.
	{Name: "xccov", Parser: XccovParser{}, Filename: "xccov.json",
		SourceExts:   []string{".swift", ".m", ".mm", ".c", ".cc", ".cpp"},
		TestSuffixes: []string{"Tests.swift", "Spec.swift", "Tests.m", "Tests.mm"}, Paths: xccovPaths, AbsolutePaths: true},
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
