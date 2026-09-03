package ignore

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	got := Parse(" cmd/preview/**\n\n# generated\r\n*_mock.go, internal/gen/* ,\n")
	want := []string{"cmd/preview/**", "*_mock.go", "internal/gen/*"}
	if !slices.Equal(got, want) {
		t.Errorf("Parse = %q, want %q", got, want)
	}
	if got := Parse(""); got != nil {
		t.Errorf("Parse(\"\") = %q, want nil", got)
	}
}

func TestMatch(t *testing.T) {
	const prefix = "github.com/acme/widgets"
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// A directory pattern matches everything under it, with or
		// without the explicit `/**`.
		{"cmd/preview/**", "github.com/acme/widgets/cmd/preview/main.go", true},
		{"cmd/preview", "github.com/acme/widgets/cmd/preview/main.go", true},
		{"cmd/preview/", "github.com/acme/widgets/cmd/preview/main.go", true},
		{"cmd/preview/**", "github.com/acme/widgets/cmd/preview/ui/page.go", true},
		{"cmd/preview", "github.com/acme/widgets/cmd/previewer/main.go", false},
		{"cmd/preview", "github.com/acme/widgets/cmd/preview.go", false},
		// Patterns float: they match at any directory level, so a path
		// under a root the pattern never mentions still lands — a module
		// path, a CI checkout directory, an absolute path.
		{"cmd/preview", "github.com/acme/widgets/internal/cmd/preview/main.go", true},
		{"src/**/*.test.ts", "/home/runner/work/r/r/src/app/routes/index.test.ts", true},
		{"src/**/*.test.ts", "C:/work/r/src/app/index.test.ts", true},
		{"src/**/*.test.ts", "src/app/routes/index.ts", false},
		{"**/cmd/preview/**", "github.com/acme/widgets/cmd/preview/main.go", true},
		{"github.com/acme/widgets/cmd/preview", "github.com/acme/widgets/cmd/preview/main.go", true},
		// A leading slash anchors: at the report path's root, or at the
		// root of the path with the upload's prefix stripped.
		{"/cmd/preview", "github.com/acme/widgets/cmd/preview/main.go", true},
		{"/cmd/preview", "github.com/acme/widgets/internal/cmd/preview/main.go", false},
		{"/github.com/acme/widgets/cmd/preview", "github.com/acme/widgets/cmd/preview/main.go", true},
		{"/src/**/*.test.ts", "/home/runner/work/r/r/src/app/index.test.ts", false},
		// A bare name matches anywhere.
		{"*_mock.go", "github.com/acme/widgets/internal/store/store_mock.go", true},
		{"*_mock.go", "github.com/acme/widgets/internal/store/store.go", false},
		{"testdata", "github.com/acme/widgets/internal/testdata/fixture.go", true},
		{"gen", "github.com/acme/widgets/internal/gen/api.go", true},
		{"gen", "github.com/acme/widgets/internal/gen.go", false},
		// `*` stays within a segment; `**` crosses them.
		{"internal/*/gen/*", "github.com/acme/widgets/internal/api/gen/types.go", true},
		{"internal/*/gen/*", "github.com/acme/widgets/internal/api/v2/gen/types.go", false},
		{"internal/**/gen/*", "github.com/acme/widgets/internal/api/v2/gen/types.go", true},
		{"internal/**/gen/*", "github.com/acme/widgets/internal/gen/types.go", true},
		{"**/*.pb.go", "github.com/acme/widgets/internal/api/v1/types.pb.go", true},
		{"internal/**", "github.com/acme/widgets/internal/a.go", true},
		{"a/**/b", "a/x/y/b/c.go", true},
		{"a/**/b", "a/x/y/c.go", false},
		// `**` inside a segment is read as `**/` + a single-star glob.
		{"internal/**.go", "github.com/acme/widgets/internal/x/y/z.go", true},
		{"internal/**.go", "github.com/acme/widgets/internal/a.go", true},
		{"internal/**.go", "github.com/acme/widgets/internal/x/y/z.ts", false},
		// `?` matches one character.
		{"internal/v?/**", "github.com/acme/widgets/internal/v2/x.go", true},
		{"internal/v?/**", "github.com/acme/widgets/internal/v10/x.go", false},
	}
	for _, tc := range tests {
		r, err := Compile([]string{tc.pattern})
		if err != nil {
			t.Errorf("Compile(%q): %v", tc.pattern, err)
			continue
		}
		if got := r.Match(tc.path, prefix); got != tc.want {
			t.Errorf("%q matches %q = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestMatchPrefix(t *testing.T) {
	r, err := Compile([]string{"/cmd/preview/**"})
	if err != nil {
		t.Fatal(err)
	}
	// Anchored at the report path only when no prefix is given...
	if r.Match("github.com/acme/widgets/cmd/preview/main.go", "") {
		t.Error("anchored pattern matched a prefixed path with no prefix given")
	}
	if !r.Match("cmd/preview/main.go", "") {
		t.Error("anchored pattern did not match the bare path")
	}
	// ...and at the prefix-stripped path when one is, trailing slash or not.
	for _, prefix := range []string{"github.com/acme/widgets", "github.com/acme/widgets/"} {
		if !r.Match("github.com/acme/widgets/cmd/preview/main.go", prefix) {
			t.Errorf("anchored pattern did not match through prefix %q", prefix)
		}
	}
	// A prefix that does not apply changes nothing.
	if r.Match("github.com/acme/widgets/cmd/preview/main.go", "example.com/other") {
		t.Error("matched through an unrelated prefix")
	}
}

func TestValidate(t *testing.T) {
	for _, bad := range []string{"[", "cmd//preview", "a/[z-"} {
		if err := Validate([]string{bad}); err == nil {
			t.Errorf("Validate(%q) accepted a bad pattern", bad)
		}
	}
	if err := Validate([]string{strings.Repeat("a", MaxPatternLength+1)}); err == nil {
		t.Error("over-long pattern accepted")
	}
	// The too-long error shows the pattern's head by rune, not by byte.
	long := strings.Repeat("ç", MaxPatternLength)
	if err := Validate([]string{long}); err == nil || !strings.Contains(err.Error(), strings.Repeat("ç", 20)) || strings.Contains(err.Error(), `\x`) {
		t.Errorf("over-long error = %v, want 20 whole runes", err)
	}
	many := make([]string, MaxPatterns+1)
	for i := range many {
		many[i] = "x"
	}
	if err := Validate(many); err == nil {
		t.Error("too many patterns accepted")
	}
	if err := Validate(many[:MaxPatterns]); err != nil {
		t.Errorf("Validate at the limit: %v", err)
	}
	if err := Validate(nil); err != nil {
		t.Errorf("Validate(nil): %v", err)
	}
}

func TestEmptyRules(t *testing.T) {
	var nilRules *Rules
	if nilRules.Match("a.go", "") {
		t.Error("nil rules must match nothing")
	}
	r, err := Compile([]string{"", "  ", "/"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Match("a.go", "") {
		t.Error("blank patterns must compile to empty rules")
	}
}

// Matching is linear: a pattern made of nothing but `**` against a deep
// path must not backtrack its way into seconds, because the pattern can
// come from any upload token holder.
func TestMatchIsLinear(t *testing.T) {
	pat := strings.Repeat("**/", 60) + "**/a/**/**/b/**/zzz"
	r, err := Compile([]string{pat})
	if err != nil {
		t.Fatal(err)
	}
	p := strings.Repeat("a/", 60) + "b.go"
	start := time.Now()
	for range 1000 {
		if r.Match(p, "") {
			t.Fatal("unexpected match")
		}
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("1000 matches took %v; the matcher is backtracking", d)
	}
}
