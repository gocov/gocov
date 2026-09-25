package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/store"
)

// Paging past the branch selector's recent fetch still finds the older
// uploads: the history reads its own window, one row beyond the page.
func TestAPIRepoUploadsPaginationPastRecentFetch(t *testing.T) {
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

	got := decodeJSON[repoUploadsDTO](t, get(f, fmt.Sprintf("/api/ui/repo-uploads/bitbucket/acme/widgets?page=%d", page)))
	if !got.HasOlder || len(got.Uploads) != uploadsPageSize {
		t.Errorf("page %d = %d uploads, older=%v; want a full page with older uploads still to come", page, len(got.Uploads), got.HasOlder)
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

// On a feature branch the PR's builds are the branch's history: the trend
// plots them with the branch's own pushes, and the summary reads the newest
// of them. What it is measured against is still a build of the branch
// itself — never a PR build, as for the pipeline's own delta and the files
// card below it.
func TestAPIRepoFeatureBranchCarriesItsPRBuilds(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "f1", "branch": "feat"}, testProfile) // push build
	doUpload(t, f, "secret-token", map[string]string{"commit": "p1", "branch": "feat", "pr_id": "7"}, testProfile)
	doUpload(t, f, "secret-token", map[string]string{"commit": "p2", "branch": "feat", "pr_id": "7"},
		"mode: set\nexample.com/m/a.go:1.1,5.2 10 3\n") // 100%

	got := decodeJSON[repoPageDTO](t, get(f, "/api/ui/repos/bitbucket/acme/widgets?branch=feat"))
	var trend []string
	for _, p := range got.Trend {
		trend = append(trend, p.SHA)
	}
	if !slices.Equal(trend, []string{"f1", "p1", "p2"}) {
		t.Errorf("trend = %v, want the push and both PR builds oldest first", trend)
	}
	if got.Summary == nil || got.Summary.Commit.SHA != "p2" {
		t.Fatalf("summary = %+v, want the newest build p2", got.Summary)
	}
	if base := got.Summary.Verdict.Base; base == nil || base.SHA != "f1" {
		t.Errorf("compared to %+v, want the branch's own push build f1, not the PR build p1", base)
	}
	if got.Files == nil || !got.Files.HasBase {
		t.Errorf("files = %+v, want them compared against a baseline too", got.Files)
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
	if got.TrendBranch != "main" {
		t.Errorf("trend branch = %q, want main for all branches", got.TrendBranch)
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
	// The branch filter moves the summary, trend and files with it.
	feat := decodeJSON[repoPageDTO](t, get(f, "/api/ui/repos/bitbucket/acme/widgets?branch=feat"))
	if feat.TrendBranch != "feat" {
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
	for _, want := range []string{`"summary":null`, `"files":null`, `"trend":[]`, `"branches":[]`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("empty repo page missing %s:\n%s", want, rec.Body)
		}
	}
	rec = get(f, "/api/ui/repo-uploads/bitbucket/acme/widgets")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"uploads":[]`) {
		t.Errorf("empty history = %d %s, want an empty list", rec.Code, rec.Body)
	}
}

// The history is the unfiltered uploads newest first, or one branch's.
func TestAPIRepoUploads(t *testing.T) {
	f := newFixture(t, nil)
	for _, u := range []struct{ commit, branch string }{{"c1", "main"}, {"f1", "feat"}, {"c2", "main"}} {
		doUpload(t, f, "secret-token", map[string]string{"commit": u.commit, "branch": u.branch}, testProfile)
	}

	all := decodeJSON[repoUploadsDTO](t, get(f, "/api/ui/repo-uploads/bitbucket/acme/widgets"))
	if shas := uploadSHAs(all.Uploads); !slices.Equal(shas, []string{"c2", "f1", "c1"}) {
		t.Errorf("history = %v, want every upload newest first", shas)
	}
	if all.HasOlder {
		t.Error("three uploads report older ones")
	}
	feat := decodeJSON[repoUploadsDTO](t, get(f, "/api/ui/repo-uploads/bitbucket/acme/widgets?branch=feat"))
	if shas := uploadSHAs(feat.Uploads); !slices.Equal(shas, []string{"f1"}) {
		t.Errorf("feat history = %v, want its one upload", shas)
	}
	if rec := get(f, "/api/ui/repo-uploads/bitbucket/acme/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown repo = %d, want 404", rec.Code)
	}
}

func uploadSHAs(rows []uploadRowDTO) []string {
	shas := make([]string, len(rows))
	for i, u := range rows {
		shas[i] = u.SHA
	}
	return shas
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
