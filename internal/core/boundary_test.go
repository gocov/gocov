package core

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// forbidden are the imports that would put transport back inside the
// coverage logic. The split is only worth having if it stays true, and
// "someone needs the request here" is exactly how it stops being true —
// so the rule is a test rather than a note in a doc comment.
var forbidden = map[string]string{
	"net/http":          "core decides, it does not serve: take what you need as arguments",
	"html/template":     "rendering belongs to internal/server",
	"net/http/httptest": "if a core test needs an HTTP server, the code under test is in the wrong package",
}

func TestCoreImportsNoTransport(t *testing.T) {
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if why, bad := forbidden[path]; bad {
					t.Errorf("%s imports %s — %s", name, path, why)
				}
				// The dependency runs one way: server calls core.
				if strings.HasSuffix(path, "/internal/server") {
					t.Errorf("%s imports internal/server; the dependency runs the other way", name)
				}
			}
		}
	}
}
