package memory

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/store"
)

// The memory store stands in for postgres in handler tests, and postgres
// hands back a fresh slice on every read. A caller that mutates what it
// got back must therefore never reach the stored row.
func TestUsersNeverAliasForgeWorkspaces(t *testing.T) {
	ctx := context.Background()
	s := New()
	u := &store.User{Forge: "github", ForgeUUID: "1", ForgeWorkspaces: []string{"acme"}, ForgeOwnedWorkspaces: []string{"acme"}}
	if err := s.UpsertUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	u.ForgeWorkspaces[0] = "mutated-after-upsert"

	got, err := s.UserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.ForgeWorkspaces, []string{"acme"}) {
		t.Fatalf("stored workspaces follow the caller's slice: %v", got.ForgeWorkspaces)
	}
	got.ForgeWorkspaces[0] = "mutated-after-read"
	got.ForgeOwnedWorkspaces[0] = "mutated-after-read"

	again, err := s.UserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.ForgeWorkspaces, []string{"acme"}) || !reflect.DeepEqual(again.ForgeOwnedWorkspaces, []string{"acme"}) {
		t.Fatalf("read result aliases the store: %v %v", again.ForgeWorkspaces, again.ForgeOwnedWorkspaces)
	}

	// Re-login replaces the snapshot; the caller's new slice must not be
	// adopted either.
	relogin := &store.User{Forge: "github", ForgeUUID: "1", ForgeWorkspaces: []string{"acme", "newco"}}
	if err := s.UpsertUser(ctx, relogin); err != nil {
		t.Fatal(err)
	}
	relogin.ForgeWorkspaces[1] = "mutated"
	final, _ := s.UserByID(ctx, u.ID)
	if !reflect.DeepEqual(final.ForgeWorkspaces, []string{"acme", "newco"}) {
		t.Fatalf("re-login snapshot aliased: %v", final.ForgeWorkspaces)
	}
}

// Names are scoped per forge, as in postgres: the same slug or prefix on
// another forge is a different row, and a workspace delete only cascades
// over its own forge's repos.
func TestNamesAreScopedPerForge(t *testing.T) {
	ctx := context.Background()
	s := New()
	bb := &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "bb-ws", DefaultBranch: "main"}
	gh := &store.Workspace{Forge: "github", Prefix: "acme", Token: "gh-ws", DefaultBranch: "main"}
	for _, ws := range []*store.Workspace{bb, gh} {
		if err := s.CreateWorkspace(ctx, ws); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateWorkspace(ctx, &store.Workspace{Forge: "github", Prefix: "acme", Token: "other", DefaultBranch: "main"}); err == nil {
		t.Error("duplicate prefix on the same forge must fail")
	}
	bbRepo := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "bb-repo", DefaultBranch: "main"}
	ghRepo := &store.Repo{Forge: "github", Slug: "acme/widgets", Token: "gh-repo", DefaultBranch: "main"}
	for _, r := range []*store.Repo{bbRepo, ghRepo} {
		if err := s.CreateRepo(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateRepo(ctx, &store.Repo{Forge: "github", Slug: "acme/widgets", Token: "other", DefaultBranch: "main"}); err == nil {
		t.Error("duplicate slug on the same forge must fail")
	}
	if got, err := s.RepoBySlug(ctx, "github", "acme/widgets"); err != nil || got.ID != ghRepo.ID {
		t.Errorf("RepoBySlug(github) = %+v, %v", got, err)
	}
	if got, err := s.WorkspaceByPrefix(ctx, "bitbucket", "acme"); err != nil || got.ID != bb.ID {
		t.Errorf("WorkspaceByPrefix(bitbucket) = %+v, %v", got, err)
	}

	if err := s.DeleteWorkspace(ctx, gh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RepoByID(ctx, ghRepo.ID); err == nil {
		t.Error("the github repo survived its workspace")
	}
	if _, err := s.RepoByID(ctx, bbRepo.ID); err != nil {
		t.Errorf("the bitbucket namesake was cascaded away: %v", err)
	}
}

// Mirrors postgres: a grant write aimed at another forge's workspace finds
// nothing to update.
func TestWorkspaceGrantStaysOnItsForge(t *testing.T) {
	ctx := context.Background()
	s := New()
	w := &store.Workspace{Forge: "gitlab", Prefix: "acme"}
	if err := s.CreateWorkspace(ctx, w); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWorkspaceGrant(ctx, w.ID, "gitlab", store.Grant{Account: "covbot"}); err != nil {
		t.Fatal(err)
	}
	for _, forge := range []string{"bitbucket", "github"} {
		if err := s.SetWorkspaceGrant(ctx, w.ID, forge, store.Grant{Account: "other"}); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("SetWorkspaceGrant(%s) on a gitlab workspace = %v, want ErrNotFound", forge, err)
		}
	}
	if got, _ := s.WorkspaceByPrefix(ctx, "gitlab", "acme"); got.Grant.Account != "covbot" {
		t.Errorf("grant = %+v, want covbot untouched", got.Grant)
	}
}

func TestUploadFileReadsOneFile(t *testing.T) {
	ctx := context.Background()
	s := New()
	u := &store.Upload{RepoID: 1, CommitSHA: "c1"}
	files := []*store.UploadFile{{Path: "a.go", Pct: 50}, {Path: "b.go", Pct: 100}}
	if err := s.CreateUpload(ctx, u, files); err != nil {
		t.Fatal(err)
	}
	got, err := s.UploadFile(ctx, u.ID, "b.go")
	if err != nil || got.Path != "b.go" || got.Pct != 100 {
		t.Errorf("UploadFile(b.go) = %+v, %v", got, err)
	}
	got.Pct = 0 // a caller's copy never reaches the stored row
	if again, _ := s.UploadFile(ctx, u.ID, "b.go"); again.Pct != 100 {
		t.Errorf("stored file changed through a returned copy: %+v", again)
	}
	for _, tc := range []struct {
		id   int64
		path string
	}{{u.ID, "c.go"}, {u.ID + 1, "a.go"}} {
		if _, err := s.UploadFile(ctx, tc.id, tc.path); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("UploadFile(%d, %s) = %v, want ErrNotFound", tc.id, tc.path, err)
		}
	}
}

// ListWorkspaceRepos is Workspace.Owns as a query: the forge must match,
// nested projects count, and LIKE metacharacters in a prefix are literal.
func TestListWorkspaceRepos(t *testing.T) {
	st := New()
	ctx := context.Background()
	for _, r := range []struct{ forge, slug string }{
		{"gitlab", "acme/api"}, {"gitlab", "acme/team/web"}, {"gitlab", "acmeco/api"},
		{"github", "acme/api"}, {"gitlab", "a_b/x"}, {"gitlab", "axb/x"},
	} {
		if err := st.CreateRepo(ctx, &store.Repo{Forge: r.forge, Slug: r.slug, Token: r.forge + r.slug, DefaultBranch: "main"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		forge, prefix string
		want          []string
	}{
		{"gitlab", "acme", []string{"acme/api", "acme/team/web"}},
		{"gitlab", "acme/team", []string{"acme/team/web"}},
		{"github", "acme", []string{"acme/api"}},
		{"gitlab", "a_b", []string{"a_b/x"}},
		{"gitlab", "nobody", nil},
	} {
		repos, err := st.ListWorkspaceRepos(ctx, tc.forge, tc.prefix)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range repos {
			got = append(got, r.Slug)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("ListWorkspaceRepos(%s, %s) = %v, want %v", tc.forge, tc.prefix, got, tc.want)
		}
	}
}

// DefaultBranchReports at limit 1 reads each repo's newest report on its own
// default branch; a repo without one is simply absent.
func TestDefaultBranchReportsLatest(t *testing.T) {
	st := New()
	ctx := context.Background()
	a := &store.Repo{Forge: "github", Slug: "acme/a", Token: "ta", DefaultBranch: "main"}
	b := &store.Repo{Forge: "github", Slug: "acme/b", Token: "tb", DefaultBranch: "trunk"}
	c := &store.Repo{Forge: "github", Slug: "acme/c", Token: "tc", DefaultBranch: "main"}
	for _, r := range []*store.Repo{a, b, c} {
		if err := st.CreateRepo(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	for _, cr := range []*store.CommitReport{
		{RepoID: a.ID, CommitSHA: "a1", Branch: "main", TotalPct: 10},
		{RepoID: a.ID, CommitSHA: "a2", Branch: "main", TotalPct: 20},
		{RepoID: a.ID, CommitSHA: "a3", Branch: "feat", TotalPct: 30},
		{RepoID: b.ID, CommitSHA: "b1", Branch: "trunk", TotalPct: 40},
		{RepoID: b.ID, CommitSHA: "b2", Branch: "main", TotalPct: 50},
		{RepoID: c.ID, CommitSHA: "c1", Branch: "feat", TotalPct: 60},
	} {
		cr.PartCount = 1
		if err := st.UpsertCommitReport(ctx, cr); err != nil {
			t.Fatal(err)
		}
	}
	got, err := latestReports(ctx, st, []int64{a.ID, b.ID, c.ID, 999})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[a.ID] == nil || got[a.ID].CommitSHA != "a2" || got[b.ID] == nil || got[b.ID].CommitSHA != "b1" {
		t.Errorf("latest default-branch reports = %v, want a2 for acme/a and b1 for acme/b only", got)
	}
}

// Mirrors postgres: PR-build reports are not the default branch's history,
// while a feature branch keeps its PR builds.
func TestDefaultBranchHistoryExcludesPRBuilds(t *testing.T) {
	ctx := context.Background()
	s := New()
	repo := &store.Repo{Forge: "github", Slug: "acme/widgets", Token: "tok", DefaultBranch: "main"}
	if err := s.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	for _, cr := range []*store.CommitReport{
		{RepoID: repo.ID, CommitSHA: "c1", Branch: "main"},
		{RepoID: repo.ID, CommitSHA: "p1", Branch: "main", PRID: "7"},
		{RepoID: repo.ID, CommitSHA: "f1", Branch: "feat", PRID: "8"},
	} {
		if err := s.UpsertCommitReport(ctx, cr); err != nil {
			t.Fatal(err)
		}
	}
	latest, _ := latestReports(ctx, s, []int64{repo.ID})
	if got := latest[repo.ID]; got == nil || got.CommitSHA != "c1" {
		t.Errorf("latest default-branch report = %+v, want c1", got)
	}
	for branch, want := range map[string]string{"main": "c1", "feat": "f1"} {
		reports, _ := s.ListBranchCommitReports(ctx, repo.ID, branch, 0)
		if len(reports) != 1 || reports[0].CommitSHA != want {
			t.Errorf("ListBranchCommitReports(%s) = %v, want only %s", branch, reports, want)
		}
	}
}

// DefaultBranchReports reads each repo's default-branch history — no other
// branch, no PR builds, newest first, capped per repo — in one call, and
// leaves diff coverage unloaded.
func TestDefaultBranchReports(t *testing.T) {
	st := New()
	ctx := context.Background()
	a := &store.Repo{Forge: "github", Slug: "acme/a", Token: "ta", DefaultBranch: "main"}
	b := &store.Repo{Forge: "github", Slug: "acme/b", Token: "tb", DefaultBranch: "trunk"}
	c := &store.Repo{Forge: "github", Slug: "acme/c", Token: "tc", DefaultBranch: "main"}
	for _, r := range []*store.Repo{a, b, c} {
		if err := st.CreateRepo(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	dc := &diffcov.Result{CoveredLines: 1, TotalLines: 2}
	for _, cr := range []*store.CommitReport{
		{RepoID: a.ID, CommitSHA: "a1", Branch: "main", DiffCoverage: dc},
		{RepoID: a.ID, CommitSHA: "a2", Branch: "main"},
		{RepoID: a.ID, CommitSHA: "a3", Branch: "main"},
		{RepoID: a.ID, CommitSHA: "ap", Branch: "main", PRID: "7"},
		{RepoID: a.ID, CommitSHA: "af", Branch: "feat"},
		{RepoID: b.ID, CommitSHA: "b1", Branch: "trunk"},
		{RepoID: c.ID, CommitSHA: "c1", Branch: "feat"},
	} {
		cr.PartCount = 1
		if err := st.UpsertCommitReport(ctx, cr); err != nil {
			t.Fatal(err)
		}
	}
	shas := func(reports []*store.CommitReport) []string {
		var out []string
		for _, cr := range reports {
			out = append(out, cr.CommitSHA)
		}
		return out
	}
	got, err := st.DefaultBranchReports(ctx, []int64{a.ID, b.ID, c.ID, 999}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !slices.Equal(shas(got[a.ID]), []string{"a3", "a2"}) || !slices.Equal(shas(got[b.ID]), []string{"b1"}) {
		t.Errorf("DefaultBranchReports = a:%v b:%v (%d repos), want a3,a2 and b1 only", shas(got[a.ID]), shas(got[b.ID]), len(got))
	}
	all, err := st.DefaultBranchReports(ctx, []int64{a.ID}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(shas(all[a.ID]), []string{"a3", "a2", "a1"}) {
		t.Errorf("uncapped = %v, want a3,a2,a1", shas(all[a.ID]))
	}
	for _, cr := range all[a.ID] {
		if cr.DiffCoverage != nil {
			t.Errorf("%s carries diff coverage; it is not loaded", cr.CommitSHA)
		}
	}
}

// latestReports is DefaultBranchReports at limit 1, keyed to the one
// report each repo has: the read the badge and the dashboard previews make.
func latestReports(ctx context.Context, st store.Store, repoIDs []int64) (map[int64]*store.CommitReport, error) {
	reports, err := st.DefaultBranchReports(ctx, repoIDs, 1)
	if err != nil {
		return nil, err
	}
	latest := map[int64]*store.CommitReport{}
	for id, rs := range reports {
		if len(rs) != 1 {
			return nil, fmt.Errorf("repo %d: %d reports at limit 1", id, len(rs))
		}
		latest[id] = rs[0]
	}
	return latest, nil
}

// LatestPassedUpload is the upload baseline as a query: the newest
// non-PR, gate-passing upload on the branch, older than beforeID when set
// and off excludeCommit when set. RecentBranches names the branches of the
// newest uploads, sorted byte-wise.
func TestLatestPassedUploadAndRecentBranches(t *testing.T) {
	st := New()
	ctx := t.Context()
	repo := &store.Repo{Forge: "github", Slug: "acme/widgets", Token: "tok", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, u := range []*store.Upload{
		{CommitSHA: "m1", Branch: "main"},
		{CommitSHA: "m2", Branch: "main", GateFailed: true},
		{CommitSHA: "p1", Branch: "main", PRID: "7"},
		{CommitSHA: "f1", Branch: "feat"},
		{CommitSHA: "m3", Branch: "main"},
		{CommitSHA: "z1", Branch: "Zeta"},
	} {
		u.RepoID, u.Format = repo.ID, "go"
		if err := st.CreateUpload(ctx, u, nil); err != nil {
			t.Fatal(err)
		}
		ids[u.CommitSHA] = u.ID
	}
	for _, tc := range []struct {
		name, branch  string
		beforeID      int64
		excludeCommit string
		want          string // "" for ErrNotFound
	}{
		{"newest passing", "main", 0, "", "m3"},
		{"older than m3 skips the PR build and the failure", "main", ids["m3"], "", "m1"},
		{"excluding m3's commit", "main", 0, "m3", "m1"},
		{"nothing older", "main", ids["m1"], "", ""},
		{"another branch", "feat", 0, "", "f1"},
		{"unknown branch", "nope", 0, "", ""},
	} {
		got, err := st.LatestPassedUpload(ctx, repo.ID, tc.branch, tc.beforeID, tc.excludeCommit)
		switch {
		case tc.want == "" && !errors.Is(err, store.ErrNotFound):
			t.Errorf("%s: got %v, %v; want ErrNotFound", tc.name, got, err)
		case tc.want != "" && (err != nil || got.CommitSHA != tc.want):
			t.Errorf("%s: got %v, %v; want %s", tc.name, got, err, tc.want)
		}
	}

	branches, err := st.RecentBranches(ctx, repo.ID, 100)
	if err != nil || !slices.Equal(branches, []string{"Zeta", "feat", "main"}) {
		t.Errorf("recent branches = %v, %v; want Zeta, feat, main", branches, err)
	}
	// Only the newest scan uploads count: z1 and m3.
	if branches, err = st.RecentBranches(ctx, repo.ID, 2); err != nil || !slices.Equal(branches, []string{"Zeta", "main"}) {
		t.Errorf("branches of the newest two = %v, %v; want Zeta, main", branches, err)
	}
}
