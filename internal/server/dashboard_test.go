package server

import (
	"strings"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/store"
)

func TestIndexListsWorkspaceRepos(t *testing.T) {
	f := newFixture(t, nil)
	second := &store.Repo{Forge: "bitbucket", Slug: "acme/gadgets", Token: "tok2", DefaultBranch: "main"}
	if err := f.store.CreateRepo(t.Context(), second); err != nil {
		t.Fatal(err)
	}

	body := doGet(t, f, "/").Body.String()
	for _, want := range []string{
		`href="/repos/acme/widgets"`, `href="/repos/acme/gadgets"`,
		`data-name="widgets"`, `data-name="gadgets"`,
		`id="repo-search"`, `id="repo-sort"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q:\n%s", want, body)
		}
	}
}

// TestDashboardNeedsAttention seeds a workspace with a failing gate, a stale
// feed and a gated-but-unwatched repo, then checks the rollups the preview
// data cannot show: the needs-attention list, the filter counts and the
// statement-weighted workspace coverage.

func TestDashboardNeedsAttention(t *testing.T) {
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

	body := doGet(t, f, "/").Body.String()
	for _, want := range []string{
		"Needs attention",
		`<span class="mono">importer</span> is failing its coverage gate`,
		"below the 60% minimum",
		`No uploads from <span class="mono">mobile</span> in 20 days`,
		`<span class="mono">android</span> has no coverage gate`,
		// repo-settings takes the slug as a trailing {slug...} wildcard, so the
		// slash rides bare — a %2F-escaped single segment 404s on a live server.
		`/repo-settings/acme/android`,
		`Failing<span class="n">1</span>`,
		`Stale<span class="n">1</span>`,
		// android has no gate; widgets has no gate and no report — both count.
		`No gate<span class="n">2</span>`,
		// weighted: (40+80+70)/300 = 63.3%
		"63.3%",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q:\n%s", want, body)
		}
	}
}

func TestIndexShowsGateAndDelta(t *testing.T) {
	f := newFixture(t, nil)
	// 80% baseline first; the gate arrives afterwards so the baseline
	// remains usable for the delta.
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)
	f.repo.Gate = store.Gate{MinCoverage: new(float64(90))}
	if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
		t.Fatal(err)
	}
	better := "mode: set\nexample.com/m/a.go:1.1,5.2 10 3\n" // 100%, passes the gate
	doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "branch": "main"}, better)

	body := doGet(t, f, "/").Body.String()
	if !strings.Contains(body, "chip pass") {
		t.Errorf("gate chip missing: %s", body)
	}
	if !strings.Contains(body, "delta up") || !strings.Contains(body, "20.0%") {
		t.Errorf("delta missing: %s", body)
	}
}

func TestIndexDeltaSkipsGateFailedBaselines(t *testing.T) {
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

	body := doGet(t, f, "/").Body.String()
	if !strings.Contains(body, "20.0%") || strings.Contains(body, "50.0%") {
		t.Errorf("delta must use the last gate-passing baseline: %s", body)
	}
}
