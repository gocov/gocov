package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/profile"
)

// aGoSource has 9 lines; testProfile marks lines 1-5 covered (count 1)
// and 7-9 uncovered, line 6 is not executable.
const aGoSource = `package m

func covered() int {
	x := 1
	return x
}
func uncovered() int {
	return 2
}
`

func sourceFixture(t *testing.T) (*fixture, int64) {
	t.Helper()
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	f.forge.Files = map[string]string{"m/a.go": aGoSource}
	rec := doUpload(t, f, "secret-token", map[string]string{
		"commit": "c1", "branch": "main", "path_prefix": "example.com",
	}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return f, resp.ID
}

func TestSourceViewCachesContent(t *testing.T) {
	f, _ := sourceFixture(t)
	get(f, "/api/ui/uploads/1/files/example.com/m/a.go")
	get(f, "/api/ui/uploads/1/files/example.com/m/a.go")
	if got := len(f.forge.FileCalls); got != 1 {
		t.Errorf("forge fetched %d times, want 1 (cache)", got)
	}
	// The cache key uses the repo-relative path at the commit.
	if _, err := f.blobs.Get(t.Context(), "source/1/c1/m/a.go"); err != nil {
		t.Errorf("source not cached: %v", err)
	}
}

func TestSourceViewFallbacks(t *testing.T) {
	unavailable := func(t *testing.T, f *fixture, path string) sourcePageDTO {
		t.Helper()
		got := decodeJSON[sourcePageDTO](t, get(f, "/api/ui/uploads/1/files/"+path))
		if got.Unavailable == "" {
			t.Fatalf("%s: source was available; wanted a reason", path)
		}
		return got
	}

	t.Run("no credentials", func(t *testing.T) {
		f := newFixture(t, nil)
		doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)
		got := unavailable(t, f, "example.com/m/a.go")
		// The uncovered summary still helps: block 7.1,9.2 is uncovered.
		if got.Uncovered != "7-9" {
			t.Errorf("uncovered ranges = %q, want them kept in the fallback", got.Uncovered)
		}
	})

	t.Run("file not on forge", func(t *testing.T) {
		f, _ := sourceFixture(t)
		if got := unavailable(t, f, "example.com/m/b.go"); !strings.Contains(got.Unavailable, "not found") {
			t.Errorf("reason = %q, want it to say the file was not found", got.Unavailable)
		}
	})

	t.Run("non-utf8 content", func(t *testing.T) {
		f, _ := sourceFixture(t)
		f.forge.Files["m/a.go"] = string([]byte{0xff, 0xfe, 0x00, 0x01})
		if got := unavailable(t, f, "example.com/m/a.go"); !strings.Contains(got.Unavailable, "not valid UTF-8") {
			t.Errorf("reason = %q", got.Unavailable)
		}
	})

	t.Run("unknown paths and uploads 404", func(t *testing.T) {
		f, _ := sourceFixture(t)
		// Dot segments are redirected away by the mux's path cleaning and
		// the cleaned URL matches no route; the API only serves paths
		// recorded in the upload, so nothing is ever exposed.
		rec := get(f, "/uploads/1/files/../../etc/passwd")
		if rec.Code == http.StatusOK {
			t.Errorf("traversal path must not be served: %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); strings.Contains(loc, "files") {
			t.Errorf("traversal redirect still points at the source view: %q", loc)
		}
		// The page route answers for the upload, so an unknown file within a
		// readable upload is the app's not-found panel over a 200 shell; an
		// unknown upload is a 404 on both surfaces.
		if rec := get(f, "/uploads/99/files/example.com/m/a.go"); rec.Code != http.StatusNotFound {
			t.Errorf("unknown upload page = %d, want 404", rec.Code)
		}
		if rec := get(f, "/api/ui/uploads/1/files/unknown.go"); rec.Code != http.StatusNotFound {
			t.Errorf("unknown file = %d, want 404", rec.Code)
		}
	})
}

func TestSourceViewSecurity(t *testing.T) {
	t.Run("dot segments in recorded paths never reach the forge", func(t *testing.T) {
		f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
		// A malicious profile records a path that would normalize into a
		// different forge API endpoint.
		evil := "mode: set\nexample.com/../../../user.go:1.1,2.2 1 1\n"
		rec := doUpload(t, f, "secret-token", map[string]string{
			"commit": "c1", "branch": "main", "path_prefix": "example.com",
		}, evil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("upload: %d %s", rec.Code, rec.Body)
		}
		get(f, "/api/ui/uploads/1/files/example.com/../../../user.go")
		// Either the mux redirects the cleaned URL away, or the handler
		// refuses to ask the forge for it — the forge must never see it.
		if len(f.forge.FileCalls) != 0 {
			t.Errorf("forge was asked for %v", f.forge.FileCalls)
		}
	})

	t.Run("forge failure detail stays out of the answer", func(t *testing.T) {
		f, _ := sourceFixture(t)
		f.forge.FileErr = errFake // "fake forge failure"
		rec := get(f, "/api/ui/uploads/1/files/example.com/m/a.go")
		if strings.Contains(rec.Body.String(), "fake forge failure") {
			t.Errorf("forge error text leaked: %s", rec.Body)
		}
		if got := decodeJSON[sourcePageDTO](t, rec); got.Unavailable != "fetching the file from the forge failed" {
			t.Errorf("reason = %q, want the generic one", got.Unavailable)
		}
	})

	t.Run("commit identifiers with separators are rejected at upload", func(t *testing.T) {
		f := newFixture(t, nil)
		for _, commit := range []string{"a/b", "a b", strings.Repeat("x", 65), "sha\n1"} {
			rec := doUpload(t, f, "secret-token", map[string]string{"commit": commit}, testProfile)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("commit %q: status = %d, want 400", commit, rec.Code)
			}
		}
	})
}

func TestSourceViewTrimsUnmappedPrefixes(t *testing.T) {
	// An upload with no stored path_prefix (uploads made before the
	// server stored prefixes, or CI checkout paths in Cobertura reports)
	// records qualified paths; the view finds the repo file by probing
	// with leading directories trimmed.
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	f.forge.Files = map[string]string{"m/a.go": aGoSource}
	rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body)
	}
	if got := decodeJSON[sourcePageDTO](t, get(f, "/api/ui/uploads/1/files/example.com/m/a.go")); got.Unavailable != "" {
		t.Errorf("trimmed lookup did not resolve the source: %q", got.Unavailable)
	}
	// Probes the recorded path first, then the trimmed variant — and
	// never a bare filename.
	want := []string{"example.com/m/a.go", "m/a.go"}
	if !reflect.DeepEqual(f.forge.FileCalls, want) {
		t.Errorf("forge calls = %v, want %v", f.forge.FileCalls, want)
	}
	// The canonical cache key serves the next view without re-probing.
	get(f, "/api/ui/uploads/1/files/example.com/m/a.go")
	if got := len(f.forge.FileCalls); got != 2 {
		t.Errorf("forge calls after cached view = %d, want 2", got)
	}

	t.Run("non-404 errors stop probing", func(t *testing.T) {
		f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
		f.forge.FileErr = errFake
		doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)
		get(f, "/api/ui/uploads/1/files/example.com/m/a.go")
		if got := len(f.forge.FileCalls); got != 1 {
			t.Errorf("forge calls = %d, want 1 (no probing after a real error)", got)
		}
	})

	t.Run("uploads with a stored prefix never probe", func(t *testing.T) {
		// When path_prefix was applied the repo path is authoritative: a
		// miss must fail closed instead of guessing a same-suffix file.
		f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
		f.forge.Files = map[string]string{"y/z.go": aGoSource}
		profileData := "mode: set\nexample.com/x/y/z.go:1.1,2.2 1 1\n"
		doUpload(t, f, "secret-token", map[string]string{
			"commit": "c1", "branch": "main", "path_prefix": "example.com",
		}, profileData)
		if got := decodeJSON[sourcePageDTO](t, get(f, "/api/ui/uploads/1/files/example.com/x/y/z.go")); got.Unavailable == "" {
			t.Error("prefixed upload must fail closed")
		}
		if !reflect.DeepEqual(f.forge.FileCalls, []string{"x/y/z.go"}) {
			t.Errorf("forge calls = %v, want just the exact path", f.forge.FileCalls)
		}
	})

	t.Run("misses are cached: no re-probing on later views", func(t *testing.T) {
		f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
		doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)
		get(f, "/api/ui/uploads/1/files/example.com/m/a.go")
		probes := len(f.forge.FileCalls)
		if probes == 0 {
			t.Fatal("expected at least one probe")
		}
		got := decodeJSON[sourcePageDTO](t, get(f, "/api/ui/uploads/1/files/example.com/m/a.go"))
		if calls := len(f.forge.FileCalls); calls != probes {
			t.Errorf("forge calls after miss-cached view = %d, want %d", calls, probes)
		}
		if !strings.Contains(got.Unavailable, "was not found at commit") {
			t.Errorf("cached miss must still explain itself: %q", got.Unavailable)
		}
	})

	t.Run("too-short trimmed matches are rejected", func(t *testing.T) {
		// testProfile covers lines up to 9; a same-suffix file with only
		// 2 lines is a collision with an unrelated file, not a match.
		f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
		f.forge.Files = map[string]string{"m/a.go": "package other\n"}
		doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)
		if got := decodeJSON[sourcePageDTO](t, get(f, "/api/ui/uploads/1/files/example.com/m/a.go")); got.Unavailable == "" {
			t.Error("short collision must not render as source")
		}
	})
}

func TestSourceCandidates(t *testing.T) {
	got := sourceCandidates("a/b/c/d.go")
	want := []string{"a/b/c/d.go", "b/c/d.go", "c/d.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
	// Single-segment paths are asked for as-is, nothing to trim.
	if got := sourceCandidates("main.go"); !reflect.DeepEqual(got, []string{"main.go"}) {
		t.Errorf("candidates = %v, want just main.go", got)
	}
	// Deep paths are capped so one page view cannot hammer the forge,
	// and the budget covers both ends: shallow trims for module paths,
	// deep trims for CI checkout prefixes.
	deep := strings.Repeat("x/", 20) + "y/z.go"
	capped := sourceCandidates(deep)
	if len(capped) != maxSourceProbes {
		t.Errorf("deep path candidates = %d, want %d", len(capped), maxSourceProbes)
	}
	if capped[len(capped)-1] != "y/z.go" {
		t.Errorf("deepest trim = %q, want the shortest suffix y/z.go", capped[len(capped)-1])
	}

	// The Jenkins-style case from review: the true repo path sits deeper
	// than the head window and must still be probed.
	jenkins := "var/lib/jenkins/workspace/acme/checkout/src/main/internal/util/strings.go"
	found := false
	for _, c := range sourceCandidates(jenkins) {
		if c == "internal/util/strings.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("deep CI prefix candidates %v miss internal/util/strings.go", sourceCandidates(jenkins))
	}
}

func TestRenderSourceLines(t *testing.T) {
	blocks := []profile.Block{
		{StartLine: 1, EndLine: 2, NumStmts: 1, Count: 3},
		{StartLine: 4, EndLine: 4, NumStmts: 1, Count: 0},
	}
	lines := renderSourceLines([]byte("a\nb\nc\nd\n"), blocks)
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4", len(lines))
	}
	if lines[0].Class != "hit" || lines[0].Count != 3 {
		t.Errorf("line 1 = %+v", lines[0])
	}
	if lines[2].Class != "" || lines[2].Count != 0 {
		t.Errorf("line 3 must be neutral: %+v", lines[2])
	}
	if lines[3].Class != "miss" {
		t.Errorf("line 4 = %+v", lines[3])
	}
	// Overlapping blocks: a line ran if any block over it did, and shows the
	// highest count.
	overlap := renderSourceLines([]byte("a\nb\n"), []profile.Block{
		{StartLine: 1, EndLine: 2, NumStmts: 1, Count: 0},
		{StartLine: 2, EndLine: 2, NumStmts: 1, Count: 5},
		{StartLine: 2, EndLine: 2, NumStmts: 1, Count: 2},
	})
	if overlap[0].Class != "miss" || overlap[1].Class != "hit" || overlap[1].Count != 5 {
		t.Errorf("overlapping blocks = %+v", overlap)
	}
	// Blocks beyond EOF must not panic.
	_ = renderSourceLines([]byte("only\n"), []profile.Block{{StartLine: 5, EndLine: 9, NumStmts: 1, Count: 1}})

	// Pre-validation rows with absurd ranges must not spin; this returns
	// promptly because the loop is clamped to the file length.
	_ = renderSourceLines([]byte("a\nb\n"), []profile.Block{{StartLine: -2_000_000_000, EndLine: 2_000_000_000, NumStmts: 1, Count: 1}})

	// CRLF sources render without the trailing carriage return.
	crlf := renderSourceLines([]byte("x\r\ny\r\n"), nil)
	if crlf[0].Text != "x" || crlf[1].Text != "y" {
		t.Errorf("crlf lines = %+v", crlf)
	}
}

func TestMarkNewlyUncovered(t *testing.T) {
	// Line 2 is uncovered now; the baseline had it covered → a regression.
	// Line 4 is uncovered now and was already uncovered → not new.
	lines := renderSourceLines([]byte("a\nb\nc\nd\n"), []profile.Block{
		{StartLine: 1, EndLine: 1, NumStmts: 1, Count: 3},
		{StartLine: 2, EndLine: 2, NumStmts: 1, Count: 0},
		{StartLine: 4, EndLine: 4, NumStmts: 1, Count: 0},
	})
	base := []profile.Block{
		{StartLine: 2, EndLine: 2, NumStmts: 1, Count: 5}, // was covered
		{StartLine: 4, EndLine: 4, NumStmts: 1, Count: 0}, // already uncovered
	}
	n := markNewlyUncovered(lines, base)
	if n != 1 {
		t.Fatalf("newly uncovered = %d, want 1", n)
	}
	if !lines[1].NewMiss {
		t.Errorf("line 2 should be newly uncovered")
	}
	if lines[3].NewMiss {
		t.Errorf("line 4 was already uncovered, not new")
	}
}

func TestAPISourceView(t *testing.T) {
	f, _ := sourceFixture(t)
	got := decodeJSON[sourcePageDTO](t, get(f, "/api/ui/uploads/1/files/example.com/m/a.go"))

	if got.Repo.Slug != "acme/widgets" || got.Upload.ID != 1 || got.Upload.SHA != "c1" {
		t.Errorf("header = %+v / %+v", got.Repo, got.Upload)
	}
	if got.File.Path != "example.com/m/a.go" || got.File.CoveredStmts != 6 || got.File.TotalStmts != 8 {
		t.Errorf("file = %+v", got.File)
	}
	if got.Unavailable != "" {
		t.Fatalf("source unavailable: %q", got.Unavailable)
	}
	if len(got.Lines) != 9 {
		t.Fatalf("lines = %d, want 9", len(got.Lines))
	}
	// Lines 1-5 ran, 7-9 did not, and line 6 is not a statement at all —
	// which is null hits, not zero.
	hits := 0
	for _, l := range got.Lines {
		if l.Hits != nil && *l.Hits > 0 {
			hits++
		}
	}
	if hits != 5 {
		t.Errorf("hit lines = %d, want 5", hits)
	}
	if got.Lines[5].Hits != nil {
		t.Errorf("non-executable line 6 = %+v, want null hits", got.Lines[5])
	}
	if got.Lines[6].Hits == nil || *got.Lines[6].Hits != 0 {
		t.Errorf("uncovered line 7 = %+v, want 0 hits", got.Lines[6])
	}
	if got.Lines[2].Text != "func covered() int {" {
		t.Errorf("line 3 text = %q", got.Lines[2].Text)
	}
	if got.Uncovered != "7-9" {
		t.Errorf("uncovered ranges = %q", got.Uncovered)
	}

	// A path the upload never recorded is a not-found, in JSON.
	rec := get(f, "/api/ui/uploads/1/files/nope.go")
	if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("unknown path: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// Without a forge to read from, the view says why instead of pretending.
func TestAPISourceViewUnavailable(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)
	rec := get(f, "/api/ui/uploads/1/files/example.com/m/a.go")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"lines":[]`) {
		t.Errorf("unavailable source did not send an empty line list:\n%s", rec.Body)
	}
	got := decodeJSON[sourcePageDTO](t, rec)
	if got.Unavailable == "" {
		t.Error("no reason given for the missing source")
	}
}
