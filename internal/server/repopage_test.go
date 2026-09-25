package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/store"
)

// The unfiltered history reuses the branch-selector fetch instead of
// querying again. On the first page whose window needs one row more than
// that fetch holds, reusing it would report no older pages with pages
// still to come.
func TestAPIRepoPaginationPastRecentFetch(t *testing.T) {
	f := newFixture(t, nil)

	page := 0
	for (page+1)*uploadsPageSize+1 <= recentUploads {
		page++
	}
	for i := range (page + 2) * uploadsPageSize {
		u := &store.Upload{
			RepoID:    f.repo.ID,
			CommitSHA: fmt.Sprintf("c%04d", i),
			Branch:    "main",
			Format:    "go",
			TotalPct:  80,
		}
		if err := f.store.CreateUpload(t.Context(), u, nil); err != nil {
			t.Fatal(err)
		}
	}

	got := decodeJSON[repoPageDTO](t, get(f, fmt.Sprintf("/api/ui/repos/bitbucket/acme/widgets?page=%d", page)))
	if got.Page != page || !got.HasOlder {
		t.Errorf("page %d reported older=%v, want older uploads still to come", got.Page, got.HasOlder)
	}
}

// The trend is the branch's own commits: a PR build is not one of them,
// and a gate failure is flagged so the chart can mark it.
func TestAPIRepoTrendSkipsPRBuilds(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"},
		"mode: set\nexample.com/m/a.go:1.1,5.2 10 3\n") // 100%
	doUpload(t, f, "secret-token", map[string]string{"commit": "pr1", "branch": "main", "pr_id": "7"}, testProfile)
	f.repo.Gate = store.Gate{MinCoverage: new(float64(90))}
	if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
		t.Fatal(err)
	}
	doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "branch": "main"}, testProfile) // 80%, fails

	got := decodeJSON[repoPageDTO](t, get(f, "/api/ui/repos/bitbucket/acme/widgets"))
	if len(got.Trend) != 2 {
		t.Fatalf("trend = %+v, want the two branch commits without the PR build", got.Trend)
	}
	if got.Trend[0].SHA != "c1" || got.Trend[1].SHA != "c2" {
		t.Errorf("trend = %+v, want c1 then c2", got.Trend)
	}
	if got.Trend[0].GateFailed || !got.Trend[1].GateFailed {
		t.Errorf("gate flags = %v/%v, want only the 80%% point failing",
			got.Trend[0].GateFailed, got.Trend[1].GateFailed)
	}
	if got.Trend[1].UploadID == 0 {
		t.Error("a trend point carries no upload to link to")
	}
}

func TestAPIRepoPage(t *testing.T) {
	f := newFixture(t, nil)
	f.repo.Gate = store.Gate{MinCoverage: new(float64(50))}
	if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
		t.Fatal(err)
	}
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"},
		"mode: set\nexample.com/m/a.go:1.1,5.2 10 3\n") // 100%
	doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "branch": "main"}, testProfile) // 80%
	doUpload(t, f, "secret-token", map[string]string{"commit": "f1", "branch": "feat"}, testProfile)

	got := decodeJSON[repoPageDTO](t, get(f, "/api/ui/repos/bitbucket/acme/widgets"))
	if got.Repo.Slug != "acme/widgets" || got.Repo.Forge != "bitbucket" {
		t.Errorf("repo = %+v", got.Repo)
	}
	if got.Repo.Gate.MinCoverage == nil || *got.Repo.Gate.MinCoverage != 50 {
		t.Errorf("gate = %+v, want the repo's minimum", got.Repo.Gate)
	}
	if !slices.Equal(got.Branches, []string{"feat", "main"}) {
		t.Errorf("branches = %v", got.Branches)
	}
	if got.Branch != "" || got.TrendBranch != "main" {
		t.Errorf("branch = %q, trend branch = %q", got.Branch, got.TrendBranch)
	}
	if got.Summary == nil {
		t.Fatal("no summary for a branch with reports")
	}
	if got.Summary.Verdict.State != "pass" || got.Summary.Verdict.Coverage != 80 {
		t.Errorf("verdict = %+v, want a passing 80%%", got.Summary.Verdict)
	}
	if got.Summary.Verdict.Delta == nil || *got.Summary.Verdict.Delta != -20 {
		t.Errorf("delta = %v, want -20", got.Summary.Verdict.Delta)
	}
	if got.Summary.Verdict.Base == nil || got.Summary.Verdict.Base.Coverage != 100 {
		t.Errorf("base = %+v, want the 100%% baseline", got.Summary.Verdict.Base)
	}
	if got.Summary.Verdict.Reason == "" {
		t.Error("verdict carries no reason")
	}
	if got.Summary.Commit.SHA != "c2" || !got.Summary.Commit.IsDefault {
		t.Errorf("commit = %+v", got.Summary.Commit)
	}
	if got.Summary.LastUpload == nil {
		t.Error("summary carries no last upload")
	}
	// The trend reads oldest first and follows the branch, not the history.
	if len(got.Trend) != 2 || got.Trend[0].Coverage != 100 || got.Trend[1].Coverage != 80 {
		t.Errorf("trend = %+v, want main's two points oldest first", got.Trend)
	}
	if got.Files == nil || len(got.Files.Files) == 0 || !got.Files.HasBase {
		t.Fatalf("files = %+v, want the latest upload's rows against a baseline", got.Files)
	}
	row := got.Files.Files[0]
	if row.Path != "example.com/m/a.go" || row.Before == nil || *row.Before != 100 {
		t.Errorf("first file row = %+v, want a.go against its 100%% baseline", row)
	}
	if row.Uncovered == "" || !row.CoverageChanged {
		t.Errorf("file row lost its uncovered ranges or its change flag: %+v", row)
	}
	// Uploads are the unfiltered history, newest first.
	if len(got.Uploads) != 3 || got.Uploads[0].SHA != "f1" {
		t.Errorf("uploads = %+v", got.Uploads)
	}
	if got.Page != 0 || got.HasOlder {
		t.Errorf("paging = page %d, older %v", got.Page, got.HasOlder)
	}
	// The branch filter moves the summary, trend and files with it.
	feat := decodeJSON[repoPageDTO](t, get(f, "/api/ui/repos/bitbucket/acme/widgets?branch=feat"))
	if feat.Branch != "feat" || feat.TrendBranch != "feat" || len(feat.Uploads) != 1 {
		t.Errorf("branch-filtered page = %+v", feat)
	}
	if len(feat.Trend) != 1 {
		t.Errorf("feat trend = %+v, want its single point", feat.Trend)
	}
}

// A repo with no uploads at all still answers with a whole page.
func TestAPIRepoPageWithoutReports(t *testing.T) {
	f := newFixture(t, nil)
	rec := get(f, "/api/ui/repos/bitbucket/acme/widgets")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{`"summary":null`, `"files":null`, `"trend":[]`, `"uploads":[]`, `"branches":[]`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("empty repo page missing %s:\n%s", want, rec.Body)
		}
	}
}

// On an open instance every viewer is a member, so the settings button
// hangs on whether a tracked workspace owns the repo.
func TestAPIRepoSettingsButtonOnOpenInstance(t *testing.T) {
	for _, tc := range []struct {
		name      string
		connected map[string]string
		want      bool
	}{
		{"untracked", nil, false},
		{"tracked", map[string]string{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.connected)
			got := decodeJSON[repoPageDTO](t, get(f, "/api/ui/repos/bitbucket/acme/widgets"))
			if got.Repo.CanSettings != tc.want {
				t.Errorf("can_settings = %v, want %v", got.Repo.CanSettings, tc.want)
			}
		})
	}
}
