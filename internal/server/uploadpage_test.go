package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/profile"
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
	body := doGet(t, f, "/uploads/1").Body.String()
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

	body := doGet(t, f, "/uploads/2").Body.String()
	for _, want := range []string{
		`class="ba"`,                // before -> after column rendered
		"100.0%",                    // a.go coverage at the baseline
		"75.0%",                     // a.go coverage now
		`class="delta`,              // per-file delta
		"7-9",                       // lines newly uncovered by this upload
		`class="verdict`,            //
		"Files this commit touched", // only-touched heading
		`class="xtra"`,              // b.go, unchanged, tucked away
		"Show all 2 files",          // reveal toggle
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

	body := doGet(t, f, "/uploads/1").Body.String()
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

	body := doGet(t, f, "/uploads/2").Body.String()
	for _, want := range []string{
		"Files this commit touched", // baseline resolved -> touched view
		`class="ba"`,                // before -> after rendered
		"100.0%",                    // a.go at the main baseline
		"75.0%",                     // a.go on the PR
	} {
		if !strings.Contains(body, want) {
			t.Errorf("PR upload page missing %q\n%s", want, body)
		}
	}
}

func TestUploadProfileDownload(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)

	rec := doGet(t, f, "/uploads/1/profile")
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "coverage.out") {
		t.Errorf("content-disposition = %q", cd)
	}
	if rec.Body.String() != testProfile {
		t.Errorf("download body does not match the uploaded profile:\n%s", rec.Body.String())
	}
	if rec := doGet(t, f, "/uploads/999/profile"); rec.Code != http.StatusNotFound {
		t.Errorf("missing upload profile: code = %d, want 404", rec.Code)
	}
}

// fakeProvider is an auth.Provider whose Identity is canned.
