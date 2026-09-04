package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestUploadHappyPath(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	rec := doUpload(t, f, "secret-token", map[string]string{
		"repo":   "acme/widgets",
		"commit": "abc123def456",
		"branch": "main",
	}, testProfile)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TotalPct != 80 || resp.CoveredStmts != 8 || resp.TotalStmts != 10 {
		t.Errorf("totals = %.1f%% %d/%d, want 80%% 8/10", resp.TotalPct, resp.CoveredStmts, resp.TotalStmts)
	}
	if resp.DeltaPct != nil {
		t.Errorf("first upload should have no delta, got %v", *resp.DeltaPct)
	}
	if resp.BuildStatus != "posted" {
		t.Errorf("build_status = %q, want posted", resp.BuildStatus)
	}

	// Stored upload and per-file rows.
	u, err := f.store.Upload(t.Context(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.CommitSHA != "abc123def456" || u.Branch != "main" || u.Format != "go" {
		t.Errorf("stored upload = %+v", u)
	}
	if u.Part != "default" {
		t.Errorf("part = %q, want default (no explicit part)", u.Part)
	}
	files, err := f.store.UploadFiles(t.Context(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if files[0].Path != "example.com/m/a.go" || files[0].Pct != 75 {
		t.Errorf("file[0] = %s %.1f%%, want a.go 75%%", files[0].Path, files[0].Pct)
	}
	if len(files[0].Blocks) != 2 {
		t.Errorf("file[0] blocks = %d, want 2 (block data must be preserved)", len(files[0].Blocks))
	}

	// Raw profile persisted in the blobstore.
	raw, err := f.blobs.Get(t.Context(), u.RawBlobKey)
	if err != nil {
		t.Fatalf("raw blob: %v", err)
	}
	if string(raw) != testProfile {
		t.Error("raw blob does not match uploaded profile")
	}

	// Build status pushed to the forge.
	if len(f.forge.StatusCalls) != 1 {
		t.Fatalf("got %d status calls, want 1", len(f.forge.StatusCalls))
	}
	call := f.forge.StatusCalls[0]
	if call.RepoSlug != "acme/widgets" || call.CommitSHA != "abc123def456" {
		t.Errorf("status call = %+v", call)
	}
	if call.Status.Description != "coverage: 80.0%" {
		t.Errorf("description = %q, want %q", call.Status.Description, "coverage: 80.0%")
	}
	if !strings.HasPrefix(call.Status.URL, "https://gocov.example/uploads/") {
		t.Errorf("status URL = %q", call.Status.URL)
	}
}

func TestUploadDelta(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	// First upload: 80%.
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1"}, testProfile)

	// Second upload: 100%.
	better := "mode: set\nexample.com/m/a.go:1.1,5.2 10 3\n"
	rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c2"}, better)
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.DeltaPct == nil || *resp.DeltaPct != 20 {
		t.Fatalf("delta = %v, want +20", resp.DeltaPct)
	}
	last := f.forge.StatusCalls[len(f.forge.StatusCalls)-1]
	if last.Status.Description != "coverage: 100.0% (+20.0%)" {
		t.Errorf("description = %q", last.Status.Description)
	}
}

func TestUploadFeatureBranchDeltaAgainstDefault(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)

	worse := "mode: set\nexample.com/m/a.go:1.1,5.2 2 1\nexample.com/m/a.go:6.1,7.2 2 0\n"
	rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "branch": "feature/x"}, worse)
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.DeltaPct == nil || *resp.DeltaPct != -30 {
		t.Fatalf("delta = %v, want -30 (50%% vs 80%% on main)", resp.DeltaPct)
	}
}

func TestUploadPartStored(t *testing.T) {
	f := newFixture(t, nil)
	rec := doUpload(t, f, "secret-token", map[string]string{
		"commit": "c1",
		"part":   "frontend",
	}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	u, err := f.store.Upload(t.Context(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Part != "frontend" {
		t.Errorf("part = %q, want frontend", u.Part)
	}

	// The server normalizes the part: "  Frontend  " trims and lowercases to
	// the same "frontend" bucket, so mixed-case callers don't split a commit.
	norm := doUpload(t, f, "secret-token", map[string]string{
		"commit": "c1",
		"part":   "  Frontend  ",
	}, testProfile)
	var nr uploadResponse
	if err := json.Unmarshal(norm.Body.Bytes(), &nr); err != nil {
		t.Fatal(err)
	}
	if nu, err := f.store.Upload(t.Context(), nr.ID); err != nil {
		t.Fatal(err)
	} else if nu.Part != "frontend" {
		t.Errorf("normalized part = %q, want frontend", nu.Part)
	}

	// Re-uploading the same part appends a fresh immutable row (uploads stay
	// append-only); the merge feature reads the latest row per part.
	rec = doUpload(t, f, "secret-token", map[string]string{
		"commit": "c1",
		"part":   "frontend",
	}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("re-upload status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp2 uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp2); err != nil {
		t.Fatal(err)
	}
	if resp2.ID == resp.ID {
		t.Errorf("re-upload reused id %d; uploads must stay append-only", resp.ID)
	}
}

// Parts covering disjoint files: backend is 8/8, frontend 0/2, so the
// merged commit is 8/10 = 80%.

func TestUploadPartsCap(t *testing.T) {
	f := newFixture(t, nil)
	for i := range maxPartsPerCommit {
		rec := doUpload(t, f, "secret-token", map[string]string{
			"commit": "c1", "part": fmt.Sprintf("p%d", i),
		}, testProfile)
		if rec.Code != http.StatusCreated {
			t.Fatalf("part %d: status %d, body %s", i, rec.Code, rec.Body)
		}
	}
	// A new part beyond the cap is rejected before any work.
	rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "part": "overflow"}, testProfile)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("part past the cap = %d, want 400", rec.Code)
	}
	// Re-uploading an existing part is still allowed — it replaces.
	rec = doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "part": "p0"}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Errorf("re-upload of an existing part = %d, want 201", rec.Code)
	}
	// A different commit is unaffected by another commit's part count.
	rec = doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "part": "fresh"}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Errorf("new commit part = %d, want 201", rec.Code)
	}
}

func TestUploadAuth(t *testing.T) {
	f := newFixture(t, nil)
	tests := []struct {
		name  string
		token string
		want  int
	}{
		{"missing token", "", http.StatusUnauthorized},
		{"wrong token", "nope", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doUpload(t, f, tt.token, map[string]string{"commit": "c"}, testProfile)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}

	t.Run("invalid token rejected before body parsing", func(t *testing.T) {
		// A garbage body with a bad token must yield 401, not 400: the
		// token check runs before the multipart parse.
		req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", strings.NewReader("not multipart"))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
		req.Header.Set("Authorization", "Bearer nope")
		rec := httptest.NewRecorder()
		f.srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401 before parsing", rec.Code)
		}
	})
}

func TestUploadValidation(t *testing.T) {
	f := newFixture(t, nil)
	tests := []struct {
		name    string
		fields  map[string]string
		profile string
		want    int
	}{
		{"repo mismatch", map[string]string{"repo": "other/repo", "commit": "c"}, testProfile, http.StatusForbidden},
		{"missing commit", map[string]string{}, testProfile, http.StatusBadRequest},
		{"missing profile file", map[string]string{"commit": "c"}, "", http.StatusBadRequest},
		{"unknown format", map[string]string{"commit": "c", "format": "opencover"}, testProfile, http.StatusBadRequest},
		{"malformed profile", map[string]string{"commit": "c"}, "not a profile", http.StatusUnprocessableEntity},
		{"part with slash", map[string]string{"commit": "c", "part": "back/end"}, testProfile, http.StatusBadRequest},
		{"part leading dash", map[string]string{"commit": "c", "part": "-backend"}, testProfile, http.StatusBadRequest},
		{"part with space inside", map[string]string{"commit": "c", "part": "back end"}, testProfile, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doUpload(t, f, "secret-token", tt.fields, tt.profile)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d; body = %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestUploadLCOV(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	// PR diff touches src/app.js lines 2 (covered) and 5 (uncovered).
	f.forge.DiffText = `diff --git a/src/app.js b/src/app.js
--- a/src/app.js
+++ b/src/app.js
@@ -1,5 +1,5 @@
 ctx
-old
+added 2
 ctx
 ctx
-old
+added 5
`
	lcov := `SF:src/app.js
DA:1,1
DA:2,1
DA:3,2
DA:5,0
end_of_record
SF:src/util.js
DA:1,0
end_of_record
`
	// No format field: the server must sniff LCOV from the content.
	rec := doUpload(t, f, "secret-token", map[string]string{
		"commit": "jsc1", "branch": "main", "pr_id": "9",
	}, lcov)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// 3 of 5 lines covered across both files.
	if resp.TotalPct != 60 || resp.CoveredStmts != 3 || resp.TotalStmts != 5 {
		t.Errorf("totals = %.1f%% %d/%d, want 60%% 3/5", resp.TotalPct, resp.CoveredStmts, resp.TotalStmts)
	}
	// Diff coverage: line 2 covered, line 5 uncovered -> 1/2. Exact path
	// match, no path_prefix needed for repo-relative lcov paths.
	if resp.DiffStatus != "computed" || resp.DiffTotalLines == nil || *resp.DiffTotalLines != 2 ||
		*resp.DiffCoveredLines != 1 {
		t.Errorf("diff = %v/%v (%s), want 1/2 computed; body = %s",
			resp.DiffCoveredLines, resp.DiffTotalLines, resp.DiffStatus, rec.Body)
	}
	// The sniffed format is what gets stored.
	u, err := f.store.Upload(t.Context(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Format != "lcov" {
		t.Errorf("stored format = %q, want lcov (sniffed)", u.Format)
	}
}

func TestUploadJaCoCo(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	// PR touches Foo.java under its source root: line 11 covered, 13 not.
	f.forge.DiffText = `diff --git a/src/main/java/com/example/app/Foo.java b/src/main/java/com/example/app/Foo.java
--- a/src/main/java/com/example/app/Foo.java
+++ b/src/main/java/com/example/app/Foo.java
@@ -10,4 +10,4 @@
 ctx
-old
+added 11
 ctx
-old
+added 13
`
	jacoco := `<?xml version="1.0" encoding="UTF-8"?>
<report name="app">
  <package name="com/example/app">
    <sourcefile name="Foo.java">
      <line nr="10" mi="0" ci="4"/>
      <line nr="11" mi="0" ci="4"/>
      <line nr="13" mi="2" ci="0"/>
    </sourcefile>
  </package>
</report>
`
	// No format field: sniffed from the XML content.
	rec := doUpload(t, f, "secret-token", map[string]string{
		"commit": "javac1", "branch": "main", "pr_id": "3",
	}, jacoco)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TotalPct != float64(2)/float64(3)*100 || resp.CoveredStmts != 2 || resp.TotalStmts != 3 {
		t.Errorf("totals = %v%% %d/%d, want 2/3", resp.TotalPct, resp.CoveredStmts, resp.TotalStmts)
	}
	// Reverse suffix matching bridges src/main/java: 1/2 changed lines.
	if resp.DiffStatus != "computed" || resp.DiffTotalLines == nil || *resp.DiffTotalLines != 2 ||
		*resp.DiffCoveredLines != 1 {
		t.Errorf("diff = %v/%v (%s), want 1/2 computed; body = %s",
			resp.DiffCoveredLines, resp.DiffTotalLines, resp.DiffStatus, rec.Body)
	}
	u, err := f.store.Upload(t.Context(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Format != "jacoco" {
		t.Errorf("stored format = %q, want jacoco (sniffed)", u.Format)
	}
}

func TestUploadCobertura(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	// PR touches myapp/app.py: line 2 covered, line 6 not.
	f.forge.DiffText = `diff --git a/myapp/app.py b/myapp/app.py
--- a/myapp/app.py
+++ b/myapp/app.py
@@ -1,6 +1,6 @@
 ctx
-old
+added 2
 ctx
 ctx
 ctx
-old
+added 6
`
	cobertura := `<?xml version="1.0" ?>
<coverage lines-valid="4" lines-covered="3" line-rate="0.75">
  <packages><package name="myapp"><classes>
    <class name="app.py" filename="myapp/app.py">
      <lines>
        <line number="1" hits="1"/>
        <line number="2" hits="4"/>
        <line number="3" hits="4"/>
        <line number="6" hits="0"/>
      </lines>
    </class>
  </classes></package></packages>
</coverage>
`
	// No format field: sniffed from the XML content.
	rec := doUpload(t, f, "secret-token", map[string]string{
		"commit": "pyc1", "branch": "main", "pr_id": "5",
	}, cobertura)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TotalPct != 75 || resp.CoveredStmts != 3 || resp.TotalStmts != 4 {
		t.Errorf("totals = %v%% %d/%d, want 75%% 3/4", resp.TotalPct, resp.CoveredStmts, resp.TotalStmts)
	}
	// Repo-relative cobertura paths match the diff exactly: 1/2.
	if resp.DiffStatus != "computed" || resp.DiffTotalLines == nil || *resp.DiffTotalLines != 2 ||
		*resp.DiffCoveredLines != 1 {
		t.Errorf("diff = %v/%v (%s), want 1/2 computed; body = %s",
			resp.DiffCoveredLines, resp.DiffTotalLines, resp.DiffStatus, rec.Body)
	}
	u, err := f.store.Upload(t.Context(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Format != "cobertura" {
		t.Errorf("stored format = %q, want cobertura (sniffed)", u.Format)
	}
}

func TestUploadClover(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	// PR touches src/Greeter.php: line 11 covered, line 14 not.
	f.forge.DiffText = `diff --git a/src/Greeter.php b/src/Greeter.php
--- a/src/Greeter.php
+++ b/src/Greeter.php
@@ -10,5 +10,5 @@
 ctx
-old
+added 11
 ctx
 ctx
-old
+added 14
`
	// PHPUnit-style clover: no clover attribute, so detection must key off
	// the <project> element under the ambiguous <coverage> root.
	clover := `<?xml version="1.0" encoding="UTF-8"?>
<coverage generated="1700000000">
  <project timestamp="1700000000">
    <file name="src/Greeter.php">
      <line num="8" type="method" name="greet" count="3"/>
      <line num="10" type="stmt" count="3"/>
      <line num="11" type="stmt" count="3"/>
      <line num="14" type="stmt" count="0"/>
    </file>
  </project>
</coverage>
`
	rec := doUpload(t, f, "secret-token", map[string]string{
		"commit": "phpc1", "branch": "main", "pr_id": "7",
	}, clover)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// Method line 8 does not count: 2 of 3 statements covered.
	if resp.CoveredStmts != 2 || resp.TotalStmts != 3 {
		t.Errorf("totals = %d/%d, want 2/3", resp.CoveredStmts, resp.TotalStmts)
	}
	if resp.DiffStatus != "computed" || resp.DiffTotalLines == nil || *resp.DiffTotalLines != 2 ||
		*resp.DiffCoveredLines != 1 {
		t.Errorf("diff = %v/%v (%s), want 1/2 computed; body = %s",
			resp.DiffCoveredLines, resp.DiffTotalLines, resp.DiffStatus, rec.Body)
	}
	u, err := f.store.Upload(t.Context(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Format != "clover" {
		t.Errorf("stored format = %q, want clover (sniffed)", u.Format)
	}
}

func TestUploadSimpleCov(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	// PR touches lib/greeter.rb: line 2 covered, line 5 not.
	f.forge.DiffText = `diff --git a/lib/greeter.rb b/lib/greeter.rb
--- a/lib/greeter.rb
+++ b/lib/greeter.rb
@@ -1,5 +1,5 @@
 ctx
-old
+added 2
 ctx
 ctx
-old
+added 5
`
	simplecov := `{
  "RSpec": {
    "coverage": {
      "lib/greeter.rb": {
        "lines": [1, 5, 5, null, 0]
      }
    },
    "timestamp": 1700000000
  }
}
`
	rec := doUpload(t, f, "secret-token", map[string]string{
		"commit": "rbc1", "branch": "main", "pr_id": "8",
	}, simplecov)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// Null line 4 is not executable: 3 of 4 lines covered.
	if resp.TotalPct != 75 || resp.CoveredStmts != 3 || resp.TotalStmts != 4 {
		t.Errorf("totals = %v%% %d/%d, want 75%% 3/4", resp.TotalPct, resp.CoveredStmts, resp.TotalStmts)
	}
	if resp.DiffStatus != "computed" || resp.DiffTotalLines == nil || *resp.DiffTotalLines != 2 ||
		*resp.DiffCoveredLines != 1 {
		t.Errorf("diff = %v/%v (%s), want 1/2 computed; body = %s",
			resp.DiffCoveredLines, resp.DiffTotalLines, resp.DiffStatus, rec.Body)
	}
	u, err := f.store.Upload(t.Context(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Format != "simplecov" {
		t.Errorf("stored format = %q, want simplecov (sniffed)", u.Format)
	}
}

const testPRDiff = `diff --git a/m/a.go b/m/a.go
--- a/m/a.go
+++ b/m/a.go
@@ -1,3 +1,5 @@
 ctx
+added 2
+added 3
 ctx
 ctx
@@ -7,2 +8,3 @@
 ctx
+added line 9
 ctx
diff --git a/m/untested.go b/m/untested.go
--- /dev/null
+++ b/m/untested.go
@@ -0,0 +1,2 @@
+l1
+l2
diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1,2 @@
 x
+docs
`

func TestUploadDiffCoverage(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	f.forge.DiffText = testPRDiff

	rec := doUpload(t, f, "secret-token", map[string]string{
		"commit": "prcommit1", "branch": "feature/x", "pr_id": "42",
	}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp.DiffStatus != "computed" {
		t.Fatalf("diff_status = %q, body = %s", resp.DiffStatus, rec.Body)
	}
	// a.go: lines 2,3 covered; line 9 ("added 8 wait no" lands on line 9,
	// inside the uncovered 7-9 block). untested.go has no profile entry.
	if resp.DiffTotalLines == nil || *resp.DiffTotalLines != 3 ||
		resp.DiffCoveredLines == nil || *resp.DiffCoveredLines != 2 {
		t.Fatalf("diff lines = %v/%v, want 2/3; body = %s",
			resp.DiffCoveredLines, resp.DiffTotalLines, rec.Body)
	}
	if resp.PRComment != "posted" {
		t.Errorf("pr_comment = %q", resp.PRComment)
	}

	// The diff was requested for the right PR.
	if len(f.forge.DiffCalls) != 1 || f.forge.DiffCalls[0].PRID != "42" {
		t.Errorf("diff calls = %+v", f.forge.DiffCalls)
	}

	// Stored upload round-trips the result.
	u, err := f.store.Upload(t.Context(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.DiffCoverage == nil || u.DiffCoverage.TotalLines != 3 {
		t.Fatalf("stored diff coverage = %+v", u.DiffCoverage)
	}
	if len(u.DiffCoverage.UnmatchedFiles) != 1 || u.DiffCoverage.UnmatchedFiles[0] != "m/untested.go" {
		t.Errorf("unmatched = %v, want [m/untested.go] (README.md filtered out)",
			u.DiffCoverage.UnmatchedFiles)
	}

	// PR comment content.
	if len(f.forge.CommentCalls) != 1 {
		t.Fatalf("comment calls = %d, want 1", len(f.forge.CommentCalls))
	}
	body := f.forge.CommentCalls[0].Body
	for _, want := range []string{
		"66.7%",         // diff pct 2/3
		"2/3",           // covered/total
		"m/a.go",        // uncovered file listed
		"m/untested.go", // unmatched file listed
		"/uploads/",     // report link
		"80.0%",         // total coverage
	} {
		if !strings.Contains(body, want) {
			t.Errorf("comment missing %q:\n%s", want, body)
		}
	}
	if f.forge.CommentCalls[0].PRID != "42" {
		t.Errorf("comment PR = %q", f.forge.CommentCalls[0].PRID)
	}
}

//go:fix inline

func TestUploadDiffCoverageErrorPaths(t *testing.T) {
	t.Run("no credentials", func(t *testing.T) {
		f := newFixture(t, nil)
		rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c", "pr_id": "1"}, testProfile)
		var resp uploadResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusCreated || !strings.HasPrefix(resp.DiffStatus, "skipped") {
			t.Errorf("code=%d diff_status=%q", rec.Code, resp.DiffStatus)
		}
		if resp.PRComment != "skipped" {
			t.Errorf("pr_comment = %q", resp.PRComment)
		}
	})

	t.Run("forge diff not implemented", func(t *testing.T) {
		f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
		// fake forge with empty DiffText returns ErrNotImplemented
		rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c", "pr_id": "1"}, testProfile)
		var resp uploadResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusCreated || resp.DiffStatus != "skipped: diff not supported by forge" {
			t.Errorf("code=%d diff_status=%q", rec.Code, resp.DiffStatus)
		}
		// Comment still posted with total coverage only.
		if resp.PRComment != "posted" {
			t.Errorf("pr_comment = %q", resp.PRComment)
		}
	})

	t.Run("diff fetch error does not fail upload", func(t *testing.T) {
		f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
		f.forge.DiffErr = errFake
		rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c", "pr_id": "1"}, testProfile)
		var resp uploadResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusCreated || !strings.HasPrefix(resp.DiffStatus, "error:") {
			t.Errorf("code=%d diff_status=%q", rec.Code, resp.DiffStatus)
		}
	})

	t.Run("non-PR upload has no diff fields", func(t *testing.T) {
		f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
		f.forge.DiffText = testPRDiff
		rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c"}, testProfile)
		var resp uploadResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.DiffStatus != "" || resp.DiffPct != nil || resp.PRComment != "" {
			t.Errorf("non-PR upload leaked diff fields: %s", rec.Body)
		}
		if len(f.forge.DiffCalls) != 0 {
			t.Errorf("diff fetched for non-PR upload")
		}
	})
}

func TestBuildUploadMeta(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/upload", nil)
	r.Form = url.Values{
		"uploader":       {"gocov v1.0.0"},
		"uploader_kind":  {"action"},
		"ci_provider":    {"GitHub"},
		"ci_run_url":     {"https://github.com/acme/widgets/actions/runs/9"},
		"commit_message": {"Fix the thing\n\nlong body dropped"},
		"commit_author":  {"  Ada  "},
	}
	m := buildUploadMeta(r, "ci/tmp/coverage.out", 2048, 1500*time.Millisecond)
	if m.ProfileName != "coverage.out" || m.ProfileBytes != 2048 || m.ProcessMillis != 1500 {
		t.Errorf("server-measured fields wrong: %+v", m)
	}
	if m.CommitMessage != "Fix the thing" || m.CommitAuthor != "Ada" {
		t.Errorf("commit fields not normalized: %+v", m)
	}
	if m.UploaderKind != "action" || m.CIProvider != "github" || m.CIRunURL == "" {
		t.Errorf("ci fields wrong: %+v", m)
	}
}

func TestBuildUploadMetaRejectsBadInput(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/upload", nil)
	r.Form = url.Values{
		"uploader_kind": {"hacker"},
		"ci_provider":   {"evil"},
		"ci_run_url":    {"javascript:alert(1)"},
	}
	m := buildUploadMeta(r, "", 0, 0)
	if m.UploaderKind != "" {
		t.Errorf("unknown uploader_kind kept: %q", m.UploaderKind)
	}
	if m.CIProvider != "" {
		t.Errorf("unknown ci_provider kept: %q", m.CIProvider)
	}
	if m.CIRunURL != "" {
		t.Errorf("non-http ci_run_url kept: %q", m.CIRunURL)
	}
}

// The repo's ignore patterns and the request's own `ignore` fields both
// trim the report before it is measured; a bad pattern is refused up front.
func TestUploadIgnorePatterns(t *testing.T) {
	f := newFixture(t, nil)
	ctx := t.Context()
	f.repo.IgnorePaths = []string{"**/b.go"}
	if err := f.store.UpdateRepo(ctx, f.repo); err != nil {
		t.Fatal(err)
	}

	// testProfile: a.go 6/8, b.go 2/2. The repo pattern drops b.go.
	rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c1"}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.IgnoredFiles != 1 || resp.TotalStmts != 8 || resp.TotalPct != 75 {
		t.Errorf("repo pattern: %+v, want 1 ignored and 6/8 = 75%%", resp)
	}
	if files, _ := f.store.UploadFiles(ctx, resp.ID); len(files) != 1 || files[0].Path != "example.com/m/a.go" {
		t.Errorf("stored files = %+v, want a.go alone", files)
	}

	// The request adds its own, matched through the path prefix, and the
	// upload page says how many files went.
	f.repo.IgnorePaths = nil
	if err := f.store.UpdateRepo(ctx, f.repo); err != nil {
		t.Fatal(err)
	}
	rec = doUpload(t, f, "secret-token", map[string]string{
		"commit": "c2", "path_prefix": "example.com", "ignore": "m/b.go, nothing/**",
	}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.IgnoredFiles != 1 || resp.TotalPct != 75 {
		t.Errorf("request pattern: %+v, want 1 ignored and 75%%", resp)
	}
	page := get(f, fmt.Sprintf("/uploads/%d", resp.ID))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "1 file ignored") {
		t.Errorf("upload page (%d) does not mention the ignored file", page.Code)
	}

	// Patterns that leave nothing to measure are refused, not landed as 0%.
	rec = doUpload(t, f, "secret-token", map[string]string{"commit": "c3", "ignore": "**"}, testProfile)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "matched every file") {
		t.Errorf("all ignored: status = %d, body = %s", rec.Code, rec.Body)
	}

	// Malformed and oversized pattern lists are 400s with the reason.
	rec = doUpload(t, f, "secret-token", map[string]string{"commit": "c3", "ignore": "src/["}, testProfile)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "ignore pattern") {
		t.Errorf("bad pattern: status = %d, body = %s", rec.Code, rec.Body)
	}
	rec = doUpload(t, f, "secret-token", map[string]string{
		"commit": "c3", "ignore": strings.Repeat("x,", 101),
	}, testProfile)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "too many ignore patterns") {
		t.Errorf("too many patterns: status = %d, body = %s", rec.Code, rec.Body)
	}
}

// A changed file that the ignore patterns dropped must not resurface as
// "changed but no coverage data": it neither counts for nor against the PR.
func TestUploadIgnoredFilesLeaveDiffCoverage(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	ctx := t.Context()
	f.repo.IgnorePaths = []string{"**/b.go", "**/untested.go"}
	if err := f.store.UpdateRepo(ctx, f.repo); err != nil {
		t.Fatal(err)
	}
	// The PR touches a.go (covered line 2), b.go (ignored, in the profile)
	// and untested.go (ignored, never in the profile).
	f.forge.DiffText = `diff --git a/m/a.go b/m/a.go
--- a/m/a.go
+++ b/m/a.go
@@ -1,3 +1,3 @@
 ctx
-old
+added 2
 ctx
diff --git a/m/b.go b/m/b.go
--- a/m/b.go
+++ b/m/b.go
@@ -1,2 +1,2 @@
-old
+added 1
 ctx
diff --git a/m/untested.go b/m/untested.go
--- a/m/untested.go
+++ b/m/untested.go
@@ -1,1 +1,1 @@
-old
+added 1
`
	rec := doUpload(t, f, "secret-token", map[string]string{
		"commit": "c1", "pr_id": "42", "path_prefix": "example.com",
	}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.IgnoredFiles != 1 {
		t.Errorf("ignored_files = %d, want 1 (b.go)", resp.IgnoredFiles)
	}
	if resp.DiffTotalLines == nil || *resp.DiffTotalLines != 1 || resp.DiffCoveredLines == nil || *resp.DiffCoveredLines != 1 {
		t.Fatalf("diff lines = %v/%v, want 1/1 (a.go only); body = %s", resp.DiffCoveredLines, resp.DiffTotalLines, rec.Body)
	}
	u, err := f.store.Upload(ctx, resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.DiffCoverage.UnmatchedFiles) != 0 {
		t.Errorf("unmatched = %v, want none: ignored files must not be flagged as untested", u.DiffCoverage.UnmatchedFiles)
	}
}
