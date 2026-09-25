package server

import (
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/store"
)

// TestAPIDashboardNeedsAttention seeds a workspace with a failing gate, a
// stale feed and a gated-but-unwatched repo, then checks the rollups one
// repo cannot show: every needs-attention cause with the numbers behind
// it, the stale count and the statement-weighted workspace coverage.
func TestAPIDashboardNeedsAttention(t *testing.T) {
	f := newFixture(t, nil) // acme/widgets exists but has no reports
	report := func(slug string, min *float64, pct float64, failed bool, age time.Duration) {
		repo := &store.Repo{Forge: "bitbucket", Slug: slug, Token: slug, DefaultBranch: "main"}
		if min != nil {
			repo.Gate = store.Gate{MinCoverage: min}
		}
		if err := f.store.CreateRepo(t.Context(), repo); err != nil {
			t.Fatal(err)
		}
		if err := f.store.UpsertCommitReport(t.Context(), &store.CommitReport{
			RepoID: repo.ID, CommitSHA: slug + "-c1", Branch: "main",
			TotalPct: pct, CoveredStmts: int64(pct), TotalStmts: 100,
			GateFailed: failed, CreatedAt: time.Now().Add(-age),
		}); err != nil {
			t.Fatal(err)
		}
	}
	min60 := 60.0
	report("acme/importer", &min60, 40, true, time.Hour)      // failing gate
	report("acme/mobile", &min60, 80, false, 20*24*time.Hour) // passing but stale
	report("acme/android", nil, 70, false, time.Hour)         // no gate

	got := decodeJSON[dashboardDTO](t, get(f, "/api/ui/dashboard"))
	byCause := map[string]attentionDTO{}
	for _, item := range got.Attention {
		byCause[item.Kind] = item
	}
	// android has no gate, and that is a choice, not an event: it stays out
	// of the list (the table's row and the No gate filter carry it).
	if len(got.Attention) != 2 || len(byCause) != 2 {
		t.Fatalf("attention = %+v, want exactly the failing and the stale notice", got.Attention)
	}
	if item := byCause["failing"]; item.Name != "importer" || item.MinCoverage == nil || *item.MinCoverage != 60 {
		t.Errorf("failing notice = %+v, want importer against its 60%% minimum", item)
	}
	if item := byCause["stale"]; item.Name != "mobile" || item.StaleDays == nil || *item.StaleDays != 20 {
		t.Errorf("stale notice = %+v, want mobile at 20 days", item)
	}
	// Most severe first, so the list reads top-down.
	if got.Attention[0].Kind != "failing" || got.Attention[1].Kind != "stale" {
		t.Errorf("attention order = %q, want failing before stale",
			[]string{got.Attention[0].Kind, got.Attention[1].Kind})
	}
	if got.Stats.StaleCount != 1 {
		t.Errorf("stale count = %d, want 1", got.Stats.StaleCount)
	}
	// Statement-weighted across the three repos that reported: (40+80+70)/300.
	if got.Stats.Coverage == nil || math.Abs(*got.Stats.Coverage-63.333) > 0.01 {
		t.Errorf("workspace coverage = %v, want the statement-weighted 63.3%%", got.Stats.Coverage)
	}
}

// The delta a row shows is measured against the last gate-passing report,
// never against a failure in between.
func TestAPIDashboardDeltaSkipsGateFailedBaselines(t *testing.T) {
	f := newFixture(t, nil)
	// 80% baseline before any gate exists.
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)
	f.repo.Gate = store.Gate{MinCoverage: new(float64(90))}
	if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
		t.Fatal(err)
	}
	// 50%: fails the gate, must never become a delta baseline.
	worse := "mode: set\nexample.com/m/a.go:1.1,5.2 1 1\nexample.com/m/a.go:6.1,7.2 1 0\n"
	doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "branch": "main"}, worse)
	// 100%: passes. Delta must be vs c1 (80%) = +20, not vs c2 (50%) = +50.
	best := "mode: set\nexample.com/m/a.go:1.1,5.2 10 3\n"
	doUpload(t, f, "secret-token", map[string]string{"commit": "c3", "branch": "main"}, best)

	got := decodeJSON[dashboardDTO](t, get(f, "/api/ui/dashboard"))
	if len(got.Repos) != 1 {
		t.Fatalf("repos = %+v, want one row", got.Repos)
	}
	if d := got.Repos[0].Delta; d == nil || *d != 20 {
		t.Errorf("delta = %v, want +20 against the last gate-passing baseline", d)
	}
}

// sparkSeries is the sparkline the app plots: the branch's own commits,
// oldest first, capped at the last dozen.
func TestSparkSeries(t *testing.T) {
	report := func(id int64, pct float64, prID string) *store.CommitReport {
		return &store.CommitReport{UploadID: id, CommitSHA: "sha", TotalPct: pct, PRID: prID}
	}
	// Reports arrive newest first; the series reads chronologically.
	got := sparkSeries([]*store.CommitReport{report(2, 90, ""), report(1, 80, "")})
	if !slices.Equal(got, []float64{80, 90}) {
		t.Errorf("series = %v, want the two points oldest first", got)
	}
	// A PR report is not one of the branch's own commits.
	got = sparkSeries([]*store.CommitReport{report(2, 90, "7"), report(1, 80, "")})
	if !slices.Equal(got, []float64{80}) {
		t.Errorf("series = %v, want the PR report excluded", got)
	}
	// Only the last dozen points survive: the first of thirteen drops off.
	var many []*store.CommitReport
	for i := range 13 {
		many = append(many, report(int64(13-i), float64(62-i), ""))
	}
	if got := sparkSeries(many); len(got) != 12 || got[0] != 51 {
		t.Errorf("series = %v, want the newest twelve points", got)
	}
}

func TestAPIDashboard(t *testing.T) {
	f := newFixture(t, nil)
	min90 := 90.0
	f.repo.Gate = store.Gate{MinCoverage: &min90}
	if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
		t.Fatal(err)
	}
	// Two reports so the row carries a series; the second fails the gate.
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"},
		"mode: set\nexample.com/m/a.go:1.1,5.2 10 3\n") // 100%, passes
	doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "branch": "main"}, testProfile) // 80%, fails

	got := decodeJSON[dashboardDTO](t, get(f, "/api/ui/dashboard"))
	if got.NeedsOnboarding {
		t.Error("open instance asked for onboarding")
	}
	if got.Current == nil || got.Current.Prefix != "acme" || got.Current.Forge != "bitbucket" {
		t.Fatalf("current workspace = %+v, want acme on bitbucket", got.Current)
	}
	if len(got.Repos) != 1 {
		t.Fatalf("repos = %+v, want one row", got.Repos)
	}
	row := got.Repos[0]
	if row.Slug != "acme/widgets" || row.Name != "widgets" {
		t.Errorf("row names = %q/%q", row.Slug, row.Name)
	}
	if row.Coverage == nil || *row.Coverage != 80 {
		t.Errorf("coverage = %v, want 80", row.Coverage)
	}
	if row.Delta == nil || *row.Delta != -20 {
		t.Errorf("delta = %v, want -20 against the passing baseline", row.Delta)
	}
	if row.Gate != "fail" {
		t.Errorf("gate = %q, want fail", row.Gate)
	}
	if !slices.Equal(row.Series, []float64{100, 80}) {
		t.Errorf("series = %v, want the branch's two points oldest first", row.Series)
	}
	if row.UploadedAt == nil {
		t.Error("row carries no upload time")
	}
	if got.Stats.GatesTotal != 1 || got.Stats.GatesPassing != 0 {
		t.Errorf("gate stats = %d/%d, want 0/1", got.Stats.GatesPassing, got.Stats.GatesTotal)
	}
	if got.Stats.Reporting != "not_connected" {
		t.Errorf("reporting = %q, want not_connected", got.Stats.Reporting)
	}
	if len(got.Attention) != 1 || got.Attention[0].Kind != "failing" {
		t.Fatalf("attention = %+v, want one failing notice", got.Attention)
	}
	item := got.Attention[0]
	if item.Slug != "acme/widgets" || item.Coverage == nil || *item.Coverage != 80 ||
		item.MinCoverage == nil || *item.MinCoverage != 90 || item.StaleDays != nil {
		t.Errorf("attention item = %+v, want the raw numbers behind the sentence", item)
	}
}

// An empty instance still answers with a whole, empty dashboard — the app
// renders the empty state from it rather than from a missing field.
func TestAPIDashboardEmpty(t *testing.T) {
	// The repo exists but belongs to no registered workspace, so the
	// signed-in user is a member of nothing and sees nothing.
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)
	sess := signIn(t, f, "/")
	rec := get(f, "/api/ui/dashboard", sess)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	// Lists are lists even when empty: the app maps over them unguarded.
	for _, want := range []string{`"repos":[]`, `"switcher":[]`, `"attention":[]`, `"current":null`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("empty dashboard missing %s:\n%s", want, rec.Body)
		}
	}
	got := decodeJSON[dashboardDTO](t, rec)
	if got.Current != nil || len(got.Repos) != 0 || len(got.Switcher) != 0 || len(got.Attention) != 0 {
		t.Errorf("empty dashboard = %+v", got)
	}
	if !got.CanOnboard {
		t.Error("a signed-in user may register a workspace")
	}
}

// A fork PR opened from the fork's "main" uploads with branch "main". It is
// not the repo's main: the dashboard row, the repo page and the badge all
// keep reading the repo's own last main build.
func TestForkPRNamedLikeTheDefaultBranchIsNotItsHistory(t *testing.T) {
	f := newFixture(t, nil)
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile) // 80%
	doUpload(t, f, "secret-token", map[string]string{"commit": "p1", "branch": "main", "pr_id": "7"},
		"mode: set\nexample.com/m/a.go:1.1,5.2 10 0\n") // 0%, from a fork's main

	dash := decodeJSON[dashboardDTO](t, get(f, "/api/ui/dashboard"))
	if len(dash.Repos) != 1 || dash.Repos[0].Coverage == nil || *dash.Repos[0].Coverage != 80 {
		t.Errorf("dashboard row = %+v, want main's own 80%%", dash.Repos)
	}
	if dash.Current == nil || dash.Current.Coverage == nil || *dash.Current.Coverage != 80 {
		t.Errorf("workspace rollup = %+v, want 80%%", dash.Current)
	}
	repo := decodeJSON[repoPageDTO](t, get(f, "/api/ui/repos/bitbucket/acme/widgets"))
	if repo.Summary == nil || repo.Summary.Commit.SHA != "c1" {
		t.Errorf("repo page summary = %+v, want commit c1", repo.Summary)
	}
	if body := get(f, "/badge/bitbucket/acme/widgets.svg").Body.String(); !strings.Contains(body, "80.0%") {
		t.Errorf("badge = %s, want 80.0%%", body)
	}
}
