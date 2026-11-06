package profile

import (
	"io"
	"path"
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
	// TestDirs are directories (with a trailing slash) whose files are
	// tests wherever they sit in the tree, for languages that put tests in
	// a directory rather than name them: Rust's tests/ and benches/,
	// Python's and Elixir's test directories, sbt's src/test/.
	TestDirs []string
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

// The other language families' source extensions; nativeExts are the
// languages one Xcode or C build compiles together.
var (
	jvmExts    = []string{".java", ".kt", ".kts", ".scala", ".groovy"}
	dotnetExts = []string{".cs", ".vb", ".fs"}
	rubyExts   = []string{".rb", ".rake"}
	elixirExts = []string{".ex", ".exs"}
	nativeExts = []string{".c", ".cc", ".cpp", ".m", ".mm", ".swift"}
)

// languageFamilies groups source extensions by the language — or the set
// of languages one build compiles together, like Swift with Objective-C —
// that a single test run measures. Every source extension of every format
// is in exactly one family.
var languageFamilies = [][]string{
	{".go"}, jsExts, jvmExts, {".py"}, dotnetExts, {".php"}, rubyExts,
	{".rs"}, {".dart"}, elixirExts, nativeExts,
}

func hasAnySuffix(path string, suffixes []string) bool {
	return slices.ContainsFunc(suffixes, func(s string) bool { return strings.HasSuffix(path, s) })
}

// familyByExt maps each family extension to its family's index.
var familyByExt = func() map[string]int {
	m := map[string]int{}
	for i, exts := range languageFamilies {
		for _, ext := range exts {
			m[ext] = i
		}
	}
	return m
}()

// familyOf returns the index of path's language family, or -1. It goes by
// the exact extension, so one family's extension ending another's (".c"
// and ".cc") cannot land a file in the wrong one.
func familyOf(p string) int {
	if i, ok := familyByExt[path.Ext(p)]; ok {
		return i
	}
	return -1
}

// ExpectsCoverage returns the test diff coverage applies to a changed
// file that report p has no data for: should p have measured it, so that
// its absence means untested code? It should when the file is one of the
// format's sources, is not a test file, and belongs to a language family
// p measures. That last part keeps formats several languages write apart:
// a frontend's LCOV says nothing about the Rust files a separate LCOV
// upload covers, so a changed .rs file is no finding against it. A format
// without source extensions expects every changed non-test file.
func (f Format) ExpectsCoverage(p *Profile) func(path string) bool {
	measured := make([]bool, len(languageFamilies)+1) // index 0: no family
	for _, file := range p.Files {
		measured[familyOf(file.Path)+1] = true
	}
	return func(path string) bool {
		switch {
		case f.IsTestFile(path):
			return false
		case len(f.SourceExts) == 0:
			return true
		case !hasAnySuffix(path, f.SourceExts):
			return false
		}
		return measured[familyOf(path)+1]
	}
}

// IsTestFile reports whether path names a test file of this format's
// languages.
func (f Format) IsTestFile(path string) bool {
	if hasAnySuffix(path, f.TestSuffixes) {
		return true
	}
	return slices.ContainsFunc(f.TestDirs, func(dir string) bool {
		return strings.HasPrefix(path, dir) || strings.Contains(path, "/"+dir)
	})
}

var formats = []Format{
	{Name: "go", Parser: GoParser{}, Filename: "coverage.out", SourceExts: []string{".go"},
		TestSuffixes: []string{"_test.go"}},
	// LCOV is JavaScript's format, and also what cargo-llvm-cov (Rust),
	// flutter test (Dart), lcov_ex (Elixir) and lcov/gcov (C, C++) write.
	{Name: "lcov", Parser: LCOVParser{}, Filename: "lcov.info",
		SourceExts:   slices.Concat(jsExts, []string{".rs", ".dart"}, elixirExts, nativeExts),
		TestSuffixes: append(jsTestSuffixes(), "_test.dart", "_test.exs"),
		TestDirs:     []string{"tests/", "test/", "benches/"}},
	{Name: "jacoco", Parser: JaCoCoParser{}, Filename: "jacoco.xml", SourceExts: jvmExts},
	{Name: "cobertura", Parser: CoberturaParser{}, Filename: "cobertura.xml",
		SourceExts: slices.Concat([]string{".py", ".cs", ".php", ".rs", ".scala"}, nativeExts),
		TestDirs:   []string{"tests/", "test/", "benches/"}},
	// Clover comes from PHPUnit, from Istanbul/Jest and from OpenClover
	// for the JVM, so it counts all three families as source.
	{Name: "clover", Parser: CloverParser{}, Filename: "clover.xml",
		SourceExts:   slices.Concat([]string{".php"}, jsExts, []string{".java", ".groovy", ".kt"}),
		TestSuffixes: jsTestSuffixes()},
	// SimpleCov writes .resultset.json; the download drops the leading dot,
	// since the name follows the commit ("abc1234-resultset.json").
	{Name: "simplecov", Parser: SimpleCovParser{}, Filename: "resultset.json", SourceExts: rubyExts,
		TestSuffixes: []string{"_spec.rb", "_test.rb"}},
	// .NET test projects live apart from the code they test and follow no
	// file-suffix convention, so OpenCover names no test files.
	{Name: "opencover", Parser: OpenCoverParser{}, Filename: "coverage.opencover.xml",
		SourceExts: dotnetExts, Paths: openCoverPaths, AbsolutePaths: true},
	// XCTest cases live in files named after the type they test plus
	// "Tests" (Xcode's template), Quick specs plus "Spec". Headers are not
	// source here: they rarely hold an executable line, so xccov seldom
	// lists them, and every changed one would read as unmeasured code.
	{Name: "xccov", Parser: XccovParser{}, Filename: "xccov.json",
		SourceExts:   nativeExts,
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
