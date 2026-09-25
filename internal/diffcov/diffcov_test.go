package diffcov

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/profile"
)

func TestParseUnifiedDiff(t *testing.T) {
	tests := []struct {
		name    string
		diff    string
		want    map[string][]int
		wantErr bool
	}{
		{
			name: "single hunk with adds and context",
			diff: `diff --git a/internal/a.go b/internal/a.go
index 111..222 100644
--- a/internal/a.go
+++ b/internal/a.go
@@ -10,4 +10,6 @@ func f() {
 context
+added 11
+added 12
 context
-removed
+added 14
`,
			want: map[string][]int{"internal/a.go": {11, 12, 14}},
		},
		{
			name: "multiple files and hunks",
			diff: `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,2 +1,3 @@
 x
+new 2
 y
@@ -10,1 +11,2 @@
 z
+new 12
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1 +1,2 @@
 a
+new 2
`,
			want: map[string][]int{"a.go": {2, 12}, "b.go": {2}},
		},
		{
			name: "new file",
			diff: `diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+l1
+l2
+l3
`,
			want: map[string][]int{"new.go": {1, 2, 3}},
		},
		{
			name: "deleted file skipped",
			diff: `diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-x
-y
`,
			want: map[string][]int{},
		},
		{
			name: "rename with edit",
			diff: `diff --git a/old.go b/renamed.go
similarity index 90%
rename from old.go
rename to renamed.go
--- a/old.go
+++ b/renamed.go
@@ -5,2 +5,3 @@
 ctx
+added 6
 ctx
`,
			want: map[string][]int{"renamed.go": {6}},
		},
		{
			name: "no newline marker ignored",
			diff: `--- a/a.go
+++ b/a.go
@@ -1 +1,2 @@
 x
+y
\ No newline at end of file
`,
			want: map[string][]int{"a.go": {2}},
		},
		{
			name: "binary file ignored",
			diff: `diff --git a/img.png b/img.png
Binary files a/img.png and b/img.png differ
`,
			want: map[string][]int{},
		},
		{
			name:    "malformed hunk header",
			diff:    "--- a/a.go\n+++ b/a.go\n@@ garbage @@\n+x\n",
			wantErr: true,
		},
		{
			name: "empty diff",
			diff: "",
			want: map[string][]int{},
		},
		{
			// A removed SQL comment renders as "--- old comment" inside the
			// hunk; it must be treated as a removed line, not a file header.
			name: "removed line rendering as --- header",
			diff: `diff --git a/migrations/0001_init.sql b/migrations/0001_init.sql
--- a/migrations/0001_init.sql
+++ b/migrations/0001_init.sql
@@ -1,4 +1,5 @@
 CREATE TABLE t (
--- old comment
+-- new comment
+  id int,
   name text
 );
`,
			want: map[string][]int{"migrations/0001_init.sql": {2, 3}},
		},
		{
			// An added line whose content starts with "++ " renders as
			// "+++ show y"; it must not clobber the current file path.
			name: "added line rendering as +++ header",
			diff: `diff --git a/a.hs b/a.hs
--- a/a.hs
+++ b/a.hs
@@ -1,2 +1,4 @@
 msg = "x"
+++ show y
+more
 end
@@ -10,1 +12,2 @@
 ctx
+tail line
`,
			want: map[string][]int{"a.hs": {2, 3, 13}},
		},
		{
			// git C-quotes non-ASCII paths: +++ "b/caf\303\251.go"
			name: "quoted non-ascii path unescaped",
			diff: "diff --git \"a/caf\\303\\251.go\" \"b/caf\\303\\251.go\"\n" +
				"--- \"a/caf\\303\\251.go\"\n" +
				"+++ \"b/caf\\303\\251.go\"\n" +
				"@@ -1 +1,2 @@\n x\n+y\n",
			want: map[string][]int{"café.go": {2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUnifiedDiff(strings.NewReader(tt.diff))
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func blocks(bs ...[4]int) []profile.Block {
	// each entry: startLine, endLine, numStmts, count
	out := make([]profile.Block, 0, len(bs))
	for _, b := range bs {
		out = append(out, profile.Block{StartLine: b[0], EndLine: b[1], NumStmts: b[2], Count: b[3]})
	}
	return out
}

func TestCompute(t *testing.T) {
	files := []FileBlocks{
		{
			Path: "github.com/x/mod/internal/a.go",
			Blocks: blocks(
				[4]int{10, 12, 2, 1}, // covered lines 10-12
				[4]int{14, 16, 2, 0}, // uncovered lines 14-16
			),
		},
		{
			Path:   "github.com/x/mod/b.go",
			Blocks: blocks([4]int{1, 5, 3, 7}),
		},
	}

	t.Run("intersection and suffix matching", func(t *testing.T) {
		added := map[string][]int{
			// 10,11 covered; 14 uncovered; 20 not executable (outside blocks)
			"internal/a.go":  {10, 11, 14, 20},
			"docs/readme.md": {1, 2},
		}
		res := Compute(files, added, "")
		if res.TotalLines != 3 || res.CoveredLines != 2 {
			t.Fatalf("totals = %d/%d, want 2/3", res.CoveredLines, res.TotalLines)
		}
		if len(res.Files) != 1 || res.Files[0].Path != "internal/a.go" {
			t.Fatalf("files = %+v", res.Files)
		}
		if !reflect.DeepEqual(res.Files[0].UncoveredLines, []int{14}) {
			t.Errorf("uncovered = %v, want [14]", res.Files[0].UncoveredLines)
		}
		if !reflect.DeepEqual(res.UnmatchedFiles, []string{"docs/readme.md"}) {
			t.Errorf("unmatched = %v", res.UnmatchedFiles)
		}
		if got := res.Percent(); got != float64(2)/float64(3)*100 {
			t.Errorf("percent = %v", got)
		}
	})

	t.Run("line in overlapping blocks covered if any counts", func(t *testing.T) {
		f := []FileBlocks{{Path: "a.go", Blocks: blocks(
			[4]int{5, 5, 1, 0},
			[4]int{5, 7, 1, 3},
		)}}
		res := Compute(f, map[string][]int{"a.go": {5}}, "")
		if res.CoveredLines != 1 || res.TotalLines != 1 {
			t.Errorf("totals = %d/%d, want 1/1", res.CoveredLines, res.TotalLines)
		}
	})

	t.Run("no executable changed lines", func(t *testing.T) {
		res := Compute(files, map[string][]int{"internal/a.go": {100, 200}}, "")
		if res.TotalLines != 0 || len(res.Files) != 0 {
			t.Errorf("res = %+v", res)
		}
		if res.Percent() != 100 {
			t.Errorf("empty diff percent = %v, want 100", res.Percent())
		}
	})

	t.Run("shortest profile path wins on ambiguous suffix", func(t *testing.T) {
		f := []FileBlocks{
			{Path: "github.com/x/vendor/github.com/y/pkg/a.go", Blocks: blocks([4]int{1, 1, 1, 0})},
			{Path: "github.com/y/pkg/a.go", Blocks: blocks([4]int{1, 1, 1, 5})},
		}
		res := Compute(f, map[string][]int{"pkg/a.go": {1}}, "")
		if res.CoveredLines != 1 {
			t.Errorf("matched the wrong file: %+v", res)
		}
	})

	t.Run("path prefix prevents false suffix match", func(t *testing.T) {
		// A brand-new untested util/errors.go must NOT bind to the covered
		// internal/util/errors.go just because the suffix matches.
		f := []FileBlocks{
			{Path: "example.com/mod/internal/util/errors.go", Blocks: blocks([4]int{1, 100, 5, 3})},
		}
		res := Compute(f, map[string][]int{"util/errors.go": {1, 2, 3}}, "example.com/mod")
		if res.TotalLines != 0 {
			t.Errorf("untested new file counted as covered: %+v", res)
		}
		if !reflect.DeepEqual(res.UnmatchedFiles, []string{"util/errors.go"}) {
			t.Errorf("unmatched = %v, want [util/errors.go]", res.UnmatchedFiles)
		}

		// The real file matches exactly through the prefix.
		res = Compute(f, map[string][]int{"internal/util/errors.go": {1, 2}}, "example.com/mod")
		if res.CoveredLines != 2 || res.TotalLines != 2 || len(res.UnmatchedFiles) != 0 {
			t.Errorf("prefixed match failed: %+v", res)
		}
	})

	t.Run("prefix with trailing slash", func(t *testing.T) {
		f := []FileBlocks{{Path: "example.com/mod/a.go", Blocks: blocks([4]int{1, 5, 1, 1})}}
		res := Compute(f, map[string][]int{"a.go": {2}}, "example.com/mod/")
		if res.CoveredLines != 1 {
			t.Errorf("trailing-slash prefix failed: %+v", res)
		}
	})

	t.Run("reverse suffix matches package-qualified paths", func(t *testing.T) {
		// JaCoCo layout: profile path is package-qualified, the diff path
		// carries the source root.
		f := []FileBlocks{
			{Path: "com/example/app/Foo.java", Blocks: blocks([4]int{10, 10, 1, 3}, [4]int{12, 12, 1, 0})},
		}
		res := Compute(f, map[string][]int{
			"src/main/java/com/example/app/Foo.java": {10, 12},
		}, "")
		if res.CoveredLines != 1 || res.TotalLines != 2 {
			t.Errorf("totals = %d/%d, want 1/2", res.CoveredLines, res.TotalLines)
		}
	})

	t.Run("reverse suffix picks the most specific profile path", func(t *testing.T) {
		f := []FileBlocks{
			{Path: "app/Foo.java", Blocks: blocks([4]int{1, 1, 1, 0})},
			{Path: "com/example/app/Foo.java", Blocks: blocks([4]int{1, 1, 1, 5})},
		}
		res := Compute(f, map[string][]int{"src/main/java/com/example/app/Foo.java": {1}}, "")
		if res.CoveredLines != 1 {
			t.Errorf("matched the wrong file: %+v", res)
		}
	})

	t.Run("bare profile basename never reverse-matches", func(t *testing.T) {
		// A default-package Main.java must not bind to every Main.java in
		// the diff.
		f := []FileBlocks{{Path: "Main.java", Blocks: blocks([4]int{1, 1, 1, 1})}}
		res := Compute(f, map[string][]int{"src/main/java/Main.java": {1}}, "")
		if res.TotalLines != 0 {
			t.Errorf("bare basename reverse match: %+v", res)
		}
	})

	t.Run("bare basename never suffix-matches without prefix", func(t *testing.T) {
		// A repo-root main.go absent from the profile must not bind to
		// some package's main.go by basename alone.
		f := []FileBlocks{{Path: "example.com/mod/cmd/x/main.go", Blocks: blocks([4]int{1, 50, 5, 2})}}
		res := Compute(f, map[string][]int{"main.go": {1, 2}}, "")
		if res.TotalLines != 0 {
			t.Errorf("basename false match: %+v", res)
		}
		if !reflect.DeepEqual(res.UnmatchedFiles, []string{"main.go"}) {
			t.Errorf("unmatched = %v, want [main.go]", res.UnmatchedFiles)
		}
	})

	t.Run("overlapping blocks: any covered block covers the line", func(t *testing.T) {
		f := []FileBlocks{{Path: "a/b.go", Blocks: blocks([4]int{1, 10, 2, 0}, [4]int{4, 5, 1, 3}, [4]int{11, 12, 1, 0})}}
		res := Compute(f, map[string][]int{"a/b.go": {3, 4, 5, 5, 12, 13}}, "")
		if res.TotalLines != 4 || res.CoveredLines != 2 {
			t.Fatalf("totals = %d/%d, want 2/4", res.CoveredLines, res.TotalLines)
		}
		if !reflect.DeepEqual(res.Files[0].UncoveredLines, []int{3, 12}) {
			t.Errorf("uncovered = %v, want [3 12]", res.Files[0].UncoveredLines)
		}
	})

	t.Run("declared span does not drive the cost", func(t *testing.T) {
		// Block ranges come from the uploader. Expanded line by line these
		// blocks are billions of iterations and would time the test out.
		var bs []profile.Block
		for col := range 500 {
			bs = append(bs, profile.Block{StartLine: 1, StartCol: col, EndLine: 5_000_000, NumStmts: 1, Count: col % 2})
		}
		res := Compute([]FileBlocks{{Path: "a/b.go", Blocks: bs}}, map[string][]int{"a/b.go": {7, 4_999_999}}, "")
		if res.TotalLines != 2 || res.CoveredLines != 2 {
			t.Errorf("totals = %d/%d, want 2/2", res.CoveredLines, res.TotalLines)
		}
	})
}

func TestResultClone(t *testing.T) {
	if (*Result)(nil).Clone() != nil {
		t.Error("nil.Clone() should be nil")
	}
	orig := &Result{
		Files: []FileCoverage{
			{Path: "a.go", CoveredLines: 1, TotalLines: 2, UncoveredLines: []int{7}},
		},
		CoveredLines:   1,
		TotalLines:     2,
		UnmatchedFiles: []string{"b.go"},
	}
	cp := orig.Clone()
	if !reflect.DeepEqual(orig, cp) {
		t.Fatalf("clone differs: %+v vs %+v", orig, cp)
	}
	cp.Files[0].UncoveredLines[0] = 99
	cp.UnmatchedFiles[0] = "mutated"
	if orig.Files[0].UncoveredLines[0] != 7 || orig.UnmatchedFiles[0] != "b.go" {
		t.Error("clone aliases the original's slices")
	}
}

func TestRanges(t *testing.T) {
	tests := []struct {
		in   []int
		want string
	}{
		{nil, ""},
		{[]int{5}, "5"},
		{[]int{5, 6, 7}, "5-7"},
		{[]int{5, 7}, "5, 7"},
		{[]int{45, 46, 47, 52, 60, 61}, "45-47, 52, 60-61"},
		{[]int{1, 1, 2}, "1-2"},
	}
	for _, tt := range tests {
		if got := Ranges(tt.in); got != tt.want {
			t.Errorf("Ranges(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDiffPathsTouches(t *testing.T) {
	diff := NewDiffPaths([]string{"internal/server/upload.go", "main.go"})
	for _, tc := range []struct {
		path, prefix string
		want         bool
	}{
		{"internal/server/upload.go", "", true},
		{"github.com/acme/widgets/internal/server/upload.go", "", true}, // module-qualified profile path
		{"github.com/acme/widgets/internal/server/upload.go", "github.com/acme/widgets", true},
		{"github.com/acme/widgets/internal/server/other.go", "github.com/acme/widgets", false},
		{"main.go", "", true},
		{"cmd/gocov/main.go", "", false}, // a bare diff name never matches by suffix
		{"cmd/gocov/main.go", "github.com/acme/widgets", false},
		{"server/upload.go", "", true}, // package-qualified: the diff path ends with it
		{"upload.go", "", false},       // ...but never a bare name
	} {
		if got := diff.Touches(tc.path, tc.prefix); got != tc.want {
			t.Errorf("Touches(%q, %q) = %v, want %v", tc.path, tc.prefix, got, tc.want)
		}
	}
	if NewDiffPaths(nil).Touches("main.go", "") {
		t.Error("an empty diff matched")
	}
}

// The indexed pairing must pick exactly what the linear scans it replaced
// picked, ties and all. Random paths over a tiny alphabet make every kind
// of collision common: exact hits, several forward candidates of one
// length, nested reverse candidates, repeated paths.
func TestIndexedPairingMatchesTheLinearRule(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	segs := []string{"a", "b", "c", "x.go", "y.go"}
	path := func() string {
		n := 1 + rng.IntN(4)
		parts := make([]string, n)
		for i := range parts {
			parts[i] = segs[rng.IntN(len(segs))]
		}
		return strings.Join(parts, "/")
	}
	for round := range 3000 {
		files := make([]FileBlocks, rng.IntN(12))
		for i := range files {
			files[i] = FileBlocks{Path: path()}
		}
		diffPaths := make([]string, 1+rng.IntN(6))
		for i := range diffPaths {
			diffPaths[i] = path()
		}
		prefix := []string{"", "", "a", "a/b/"}[rng.IntN(4)]

		idx := newProfileIndex(files, prefix)
		touched := NewDiffPaths(diffPaths)
		for _, dp := range diffPaths {
			if got, want := idx.match(dp, prefix), linearMatchFile(files, dp, prefix); got != want {
				t.Fatalf("round %d: match(%q, prefix %q) over %v = %v, want %v", round, dp, prefix, files, got, want)
			}
		}
		for _, f := range files {
			if got, want := touched.Touches(f.Path, prefix), linearTouches(f.Path, prefix, diffPaths); got != want {
				t.Fatalf("round %d: Touches(%q, prefix %q) over %v = %v, want %v", round, f.Path, prefix, diffPaths, got, want)
			}
		}
	}
}

// linearMatchFile is the pairing as it was written before the index: a
// scan of every profile file per diff path.
func linearMatchFile(files []FileBlocks, diffPath, pathPrefix string) *FileBlocks {
	if pathPrefix != "" {
		want := strings.TrimSuffix(pathPrefix, "/") + "/" + diffPath
		for i := range files {
			if files[i].Path == want || files[i].Path == diffPath {
				return &files[i]
			}
		}
		return nil
	}
	var forward, reverse *FileBlocks
	for i := range files {
		fb := &files[i]
		if fb.Path == diffPath {
			return fb
		}
		if strings.Contains(diffPath, "/") && strings.HasSuffix(fb.Path, "/"+diffPath) {
			if forward == nil || len(fb.Path) < len(forward.Path) {
				forward = fb
			}
		}
		if strings.Contains(fb.Path, "/") && strings.HasSuffix(diffPath, "/"+fb.Path) {
			if reverse == nil || len(fb.Path) > len(reverse.Path) {
				reverse = fb
			}
		}
	}
	if forward != nil {
		return forward
	}
	return reverse
}

// linearTouches is the server's isSourceChanged as it was written before
// DiffPaths: a scan of every diff path per profile file.
func linearTouches(fPath, pathPrefix string, diffPaths []string) bool {
	diffFiles := map[string]bool{}
	for _, p := range diffPaths {
		diffFiles[p] = true
	}
	if diffFiles[fPath] {
		return true
	}
	if pathPrefix != "" {
		repoPath, _ := strings.CutPrefix(fPath, strings.TrimSuffix(pathPrefix, "/")+"/")
		return diffFiles[repoPath]
	}
	for dp := range diffFiles {
		if strings.Contains(dp, "/") && strings.HasSuffix(fPath, "/"+dp) {
			return true
		}
		if strings.Contains(fPath, "/") && strings.HasSuffix(dp, "/"+fPath) {
			return true
		}
	}
	return false
}
