package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

func TestUncoveredRanges(t *testing.T) {
	b := func(start, end, stmts, count int) profile.Block {
		return profile.Block{StartLine: start, EndLine: end, NumStmts: stmts, Count: count}
	}
	tests := []struct {
		name   string
		blocks []profile.Block
		want   string
	}{
		{"fully covered", []profile.Block{b(1, 5, 3, 2)}, ""},
		{"single line", []profile.Block{b(7, 7, 1, 0)}, "7"},
		{"range", []profile.Block{b(10, 14, 3, 0)}, "10-14"},
		{"adjacent blocks merge", []profile.Block{b(10, 12, 2, 0), b(13, 15, 2, 0)}, "10-15"},
		{"overlapping blocks merge", []profile.Block{b(10, 14, 2, 0), b(12, 16, 2, 0)}, "10-16"},
		{"mixed with covered", []profile.Block{b(1, 5, 3, 9), b(8, 9, 2, 0), b(20, 20, 1, 0)}, "8-9, 20"},
		{"unsorted input", []profile.Block{b(20, 22, 1, 0), b(3, 4, 1, 0)}, "3-4, 20-22"},
		{"zero statement blocks ignored", []profile.Block{b(5, 6, 0, 0)}, ""},
		{
			"capped with more marker",
			[]profile.Block{b(1, 1, 1, 0), b(3, 3, 1, 0), b(5, 5, 1, 0), b(7, 7, 1, 0),
				b(9, 9, 1, 0), b(11, 11, 1, 0), b(13, 13, 1, 0), b(15, 15, 1, 0)},
			"1, 3, 5, 7, 9, 11, +2 more",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uncoveredRanges(tt.blocks); got != tt.want {
				t.Errorf("uncoveredRanges() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestIndexListsWorkspaceRepos checks that both repos of a workspace render on
// the dashboard, each carrying the data-name hook the client-side search/sort
// operate on. (Search, filter and sort run in the browser over these rows, so
// there is no server-side ?q filter to assert.)

func TestUploadPageShowsUncoveredRanges(t *testing.T) {
	f := newFixture(t, nil)
	// a.go block 7.1,9.2 is uncovered in testProfile -> "7-9".
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)
	body := get(f, "/uploads/1").Body.String()
	if !strings.Contains(body, `class="uncov"`) || !strings.Contains(body, "7-9") {
		t.Errorf("uncovered ranges missing: %s", body)
	}
}

// testProfileFull covers a.go's 7-9 block that testProfile leaves uncovered,
// so a following testProfile upload reads as a regression: a.go drops
// 100% -> 75% and lines 7-9 become newly uncovered.

const testProfileFull = `mode: set
example.com/m/a.go:1.1,5.2 6 1
example.com/m/a.go:7.1,9.2 2 1
example.com/m/b.go:1.1,3.2 2 1
`

func TestUploadPageBeforeAfter(t *testing.T) {
	f := newFixture(t, nil)
	// A passing baseline on main, then a regressing head upload.
	doUpload(t, f, "secret-token", map[string]string{"commit": "base1", "branch": "main"}, testProfileFull)
	doUpload(t, f, "secret-token", map[string]string{"commit": "head1", "branch": "main"}, testProfile)

	body := get(f, "/uploads/2").Body.String()
	for _, want := range []string{
		`class="ba"`,                            // before -> after column rendered
		"100.0%",                                // a.go coverage at the baseline
		"75.0%",                                 // a.go coverage now
		`class="delta`,                          // per-file delta
		"7-9",                                   // lines newly uncovered by this upload
		`class="verdict`,                        //
		`aria-pressed="true" data-filter="all"`, // every file listed by default
		`Changed<span class="n">1</span>`,       // a.go is the one file that moved
		`data-changed="false"`,                  // b.go, unchanged, still listed
	} {
		if !strings.Contains(body, want) {
			t.Errorf("upload page missing %q\n%s", want, body)
		}
	}
}

func TestUploadPageShowsProvenance(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{
		"commit": "c1", "branch": "main",
		"commit_message": "Fix the ledger reconcile",
		"commit_author":  "Ada Lovelace",
		"uploader":       "gocov v1.2.3",
		"uploader_kind":  "action",
		"ci_provider":    "github",
		"ci_run_url":     "https://github.com/acme/widgets/actions/runs/7",
	}, testProfile)

	body := get(f, "/uploads/1").Body.String()
	for _, want := range []string{
		"Fix the ledger reconcile", // commit subject as the heading
		"Ada Lovelace",             // author
		"GitHub Actions",           // CI provider label
		"view run",                 // CI run link
		"gocov v1.2.3",             // uploader
		"coverage.out",             // profile filename
	} {
		if !strings.Contains(body, want) {
			t.Errorf("upload page missing %q\n%s", want, body)
		}
	}
}

func TestUploadPagePRBaselinesAgainstDefaultBranch(t *testing.T) {
	f := newFixture(t, nil)
	// A passing baseline on main, then a PR upload on a feature branch with
	// no prior upload of its own: it must still show before -> after, baselined
	// against main.
	doUpload(t, f, "secret-token", map[string]string{"commit": "main1", "branch": "main"}, testProfileFull)
	doUpload(t, f, "secret-token", map[string]string{"commit": "pr1", "branch": "feature/x", "pr_id": "7"}, testProfile)

	body := get(f, "/uploads/2").Body.String()
	for _, want := range []string{
		`id="file-filters"`, // baseline resolved -> filter tabs rendered
		`class="ba"`,        // before -> after rendered
		"100.0%",            // a.go at the main baseline
		"75.0%",             // a.go on the PR
	} {
		if !strings.Contains(body, want) {
			t.Errorf("PR upload page missing %q\n%s", want, body)
		}
	}
}

func TestUploadProfileDownload(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)

	rec := get(f, "/uploads/1/profile")
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "coverage.out") {
		t.Errorf("content-disposition = %q", cd)
	}
	if rec.Body.String() != testProfile {
		t.Errorf("download body does not match the uploaded profile:\n%s", rec.Body.String())
	}
	if rec := get(f, "/uploads/999/profile"); rec.Code != http.StatusNotFound {
		t.Errorf("missing upload profile: code = %d, want 404", rec.Code)
	}
}

func TestUploadPageTreeAndFilters(t *testing.T) {
	f := newFixture(t, nil)
	// Base upload on main
	doUpload(t, f, "secret-token", map[string]string{"commit": "base1", "branch": "main"}, testProfileFull)
	// Head upload on main: a.go coverage drops (Coverage changed)
	doUpload(t, f, "secret-token", map[string]string{"commit": "head1", "branch": "main"}, testProfile)

	body := get(f, "/uploads/2").Body.String()
	for _, want := range []string{
		`id="view-mode"`,
		`data-view="tree"`,
		`data-view="list"`,
		`id="file-filters"`,
		`data-filter="all"`,
		`data-filter="changed"`,
		`data-filter="source"`,
		`data-filter="coverage"`,
		`id="file-search"`,
		`id="filetable-tree"`,
		`class="tree-row tree-dir`,
		`class="tree-row tree-file`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("upload page missing %q in response", want)
		}
	}
}

func TestBuildFileTree(t *testing.T) {
	rows := []uploadFileRow{
		{
			UploadFile: &store.UploadFile{Path: "cmd/gocov/client.go", CoveredStmts: 8, TotalStmts: 10, Pct: 80.0},
			Dir:        "cmd/gocov/",
			Base:       "client.go",
		},
		{
			UploadFile: &store.UploadFile{Path: "cmd/gocov/main.go", CoveredStmts: 12, TotalStmts: 20, Pct: 60.0},
			Dir:        "cmd/gocov/",
			Base:       "main.go",
		},
		{
			UploadFile: &store.UploadFile{Path: "internal/server/upload.go", CoveredStmts: 30, TotalStmts: 40, Pct: 75.0},
			Dir:        "internal/server/",
			Base:       "upload.go",
		},
	}
	tree := buildFileTree(rows, false)
	if len(tree) == 0 {
		t.Fatal("expected non-empty tree")
	}

	var foundCmdGocov, foundInternalServer bool
	for _, tr := range tree {
		if tr.IsDir && tr.Path == "cmd/gocov" {
			foundCmdGocov = true
			if tr.TotalStmts != 30 || tr.CoveredStmts != 20 {
				t.Errorf("cmd/gocov stmts = %d/%d, want 20/30", tr.CoveredStmts, tr.TotalStmts)
			}
			expectedPct := float64(20) / float64(30) * 100
			if tr.Pct != expectedPct {
				t.Errorf("cmd/gocov pct = %f, want %f", tr.Pct, expectedPct)
			}
		}
		if tr.IsDir && tr.Path == "internal/server" {
			foundInternalServer = true
			if tr.TotalStmts != 40 || tr.CoveredStmts != 30 {
				t.Errorf("internal/server stmts = %d/%d, want 30/40", tr.CoveredStmts, tr.TotalStmts)
			}
		}
	}
	if !foundCmdGocov {
		t.Error("cmd/gocov directory row not found in tree")
	}
	if !foundInternalServer {
		t.Error("internal/server directory row not found in tree")
	}
}

func TestIsSourceChanged(t *testing.T) {
	diff := map[string]bool{"internal/server/upload.go": true, "main.go": true}
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
		{"internal/server/upload.go", "", true},
	} {
		if got := isSourceChanged(tc.path, tc.prefix, diff); got != tc.want {
			t.Errorf("isSourceChanged(%q, %q) = %v, want %v", tc.path, tc.prefix, got, tc.want)
		}
	}
	if isSourceChanged("main.go", "", nil) {
		t.Error("nil diff set matched")
	}
}

func TestBuildFileTreeFoldsUnchangedDirectories(t *testing.T) {
	file := func(path string, changed bool) uploadFileRow {
		dir, base := splitPath(path)
		return uploadFileRow{
			UploadFile: &store.UploadFile{Path: path, CoveredStmts: 1, TotalStmts: 2, Pct: 50},
			Dir:        dir, Base: base, Changed: changed,
		}
	}
	state := func(rows []treeRow) map[string]string {
		got := map[string]string{}
		for _, r := range rows {
			s := "closed"
			if r.Open {
				s = "open"
			}
			if r.Hidden {
				s += ",hidden"
			}
			got[r.Path] = s
		}
		return got
	}

	// With a baseline and a change, only the path to the change is open.
	got := state(buildFileTree([]uploadFileRow{
		file("internal/api/handler.go", false),
		file("internal/billing/charge.go", true),
		file("cmd/gocov/main.go", false),
	}, true))
	for path, want := range map[string]string{
		"internal":                   "open",
		"internal/billing":           "open",
		"internal/billing/charge.go": "closed",
		"internal/api":               "closed",
		"internal/api/handler.go":    "closed,hidden",
		"cmd/gocov":                  "closed",
		"cmd/gocov/main.go":          "closed,hidden",
	} {
		if got[path] != want {
			t.Errorf("%s: got %q, want %q", path, got[path], want)
		}
	}

	// Without any change the top level is open and everything below folded.
	got = state(buildFileTree([]uploadFileRow{
		file("internal/api/handler.go", false),
		file("internal/billing/charge.go", false),
	}, false))
	for path, want := range map[string]string{
		"internal":                   "open",
		"internal/api":               "closed",
		"internal/api/handler.go":    "closed,hidden",
		"internal/billing":           "closed",
		"internal/billing/charge.go": "closed,hidden",
	} {
		if got[path] != want {
			t.Errorf("no-base %s: got %q, want %q", path, got[path], want)
		}
	}
}

func TestUploadPageDiffCoverageSourceChanged(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	f.forge.DiffText = testPRDiff

	doUpload(t, f, "secret-token", map[string]string{"commit": "base1", "branch": "main"}, testProfileFull)
	doUpload(t, f, "secret-token", map[string]string{
		"commit": "prcommit1", "branch": "feature/x", "pr_id": "42",
	}, testProfile)

	body := get(f, "/uploads/2").Body.String()
	if !strings.Contains(body, `Source Changed<span class="n">1</span>`) {
		t.Errorf("expected Source Changed to have count 1, got body:\n%s", body)
	}
	if !strings.Contains(body, `data-source="true"`) {
		t.Errorf("expected row with data-source=true, got body:\n%s", body)
	}
}

// fakeProvider is an auth.Provider whose Identity is canned.
