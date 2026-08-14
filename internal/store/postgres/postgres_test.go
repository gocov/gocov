package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/secretbox"
	"github.com/gocov/gocov/internal/store"
	"github.com/gocov/gocov/internal/store/postgres"
	"github.com/gocov/gocov/internal/testpg"
)

func newTestStore(t *testing.T) *postgres.Store {
	t.Helper()
	st := postgres.New(testpg.Pool(t))
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Migrations must be idempotent across restarts.
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	return st
}

func TestRepoLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	repo := &store.Repo{
		Forge:            "bitbucket",
		Slug:             "acme/widgets",
		Token:            "tok-1",
		DefaultBranch:    "main",
		ForgeCredentials: map[string]string{"username": "u", "app_password": "p"},
	}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if repo.ID == 0 || repo.CreatedAt.IsZero() {
		t.Fatalf("CreateRepo did not fill ID/CreatedAt: %+v", repo)
	}

	// All lookups return the same row, credentials included.
	for name, get := range map[string]func() (*store.Repo, error){
		"by id":    func() (*store.Repo, error) { return st.RepoByID(ctx, repo.ID) },
		"by slug":  func() (*store.Repo, error) { return st.RepoBySlug(ctx, "acme/widgets") },
		"by token": func() (*store.Repo, error) { return st.RepoByToken(ctx, "tok-1") },
	} {
		got, err := get()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Slug != repo.Slug || got.Token != repo.Token || got.DefaultBranch != "main" ||
			!reflect.DeepEqual(got.ForgeCredentials, repo.ForgeCredentials) {
			t.Errorf("%s: got %+v", name, got)
		}
	}

	// Unique constraints hold.
	dup := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "other", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, dup); err == nil {
		t.Error("duplicate slug must fail")
	}

	// ListRepos is sorted by slug.
	second := &store.Repo{Forge: "bitbucket", Slug: "aaa/first", Token: "tok-2", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, second); err != nil {
		t.Fatal(err)
	}
	repos, err := st.ListRepos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].Slug != "aaa/first" || repos[1].Slug != "acme/widgets" {
		t.Errorf("ListRepos = %v, %v", repos[0].Slug, repos[1].Slug)
	}

	// Update: branch change + credential clearing round-trips through JSONB,
	// and nullable gate fields survive set/clear cycles.
	minCov, maxDrop := 82.5, 0.0
	repo.DefaultBranch = "develop"
	repo.ForgeCredentials = nil
	repo.Token = "tok-rotated"
	repo.Gate = store.Gate{MinCoverage: &minCov, MaxCoverageDrop: &maxDrop}
	if err := st.UpdateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	got, err := st.RepoByID(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultBranch != "develop" || got.ForgeCredentials != nil || got.Token != "tok-rotated" {
		t.Errorf("after update: %+v", got)
	}
	if got.Gate.MinCoverage == nil || *got.Gate.MinCoverage != 82.5 ||
		got.Gate.MaxCoverageDrop == nil || *got.Gate.MaxCoverageDrop != 0 ||
		got.Gate.MinDiffCoverage != nil {
		t.Errorf("gate round trip: %+v", got.Gate)
	}
	repo.Gate = store.Gate{}
	if err := st.UpdateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.RepoByID(ctx, repo.ID); got.Gate.Configured() {
		t.Errorf("gate not cleared: %+v", got.Gate)
	}
	if _, err := st.RepoByToken(ctx, "tok-1"); !errors.Is(err, store.ErrNotFound) {
		t.Error("old token still resolves after rotation")
	}

	// Missing rows yield ErrNotFound.
	if err := st.UpdateRepo(ctx, &store.Repo{ID: 9999, Slug: "x/y", Token: "t"}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("UpdateRepo missing = %v", err)
	}
	if _, err := st.RepoBySlug(ctx, "no/such"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("RepoBySlug missing = %v", err)
	}
}

func TestWorkspaceLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	minDiff := 70.0
	w := &store.Workspace{
		Forge: "bitbucket", Prefix: "acme", Token: "ws-tok", DefaultBranch: "development",
		ForgeCredentials:     map[string]string{"username": "bot", "app_password": "pw"},
		Gate:                 store.Gate{MinDiffCoverage: &minDiff},
		GitHubInstallationID: 42,
	}
	if err := st.CreateWorkspace(ctx, w); err != nil {
		t.Fatal(err)
	}
	if w.ID == 0 || w.CreatedAt.IsZero() {
		t.Fatalf("CreateWorkspace did not fill ID/CreatedAt: %+v", w)
	}

	for name, get := range map[string]func() (*store.Workspace, error){
		"by prefix": func() (*store.Workspace, error) { return st.WorkspaceByPrefix(ctx, "acme") },
		"by token":  func() (*store.Workspace, error) { return st.WorkspaceByToken(ctx, "ws-tok") },
	} {
		got, err := get()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Prefix != "acme" || got.DefaultBranch != "development" ||
			!reflect.DeepEqual(got.ForgeCredentials, w.ForgeCredentials) {
			t.Errorf("%s: %+v", name, got)
		}
		if got.Gate.MinDiffCoverage == nil || *got.Gate.MinDiffCoverage != 70 {
			t.Errorf("%s gate: %+v", name, got.Gate)
		}
		if got.GitHubInstallationID != 42 || got.GitHubAppBroken {
			t.Errorf("%s github app link: id = %d, broken = %v", name, got.GitHubInstallationID, got.GitHubAppBroken)
		}
	}

	// Unique constraints.
	if err := st.CreateWorkspace(ctx, &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "other", DefaultBranch: "main"}); err == nil {
		t.Error("duplicate prefix must fail")
	}
	if err := st.CreateWorkspace(ctx, &store.Workspace{Forge: "bitbucket", Prefix: "beta", Token: "ws-tok", DefaultBranch: "main"}); err == nil {
		t.Error("duplicate token must fail")
	}

	// List is sorted by prefix.
	if err := st.CreateWorkspace(ctx, &store.Workspace{Forge: "bitbucket", Prefix: "aaa", Token: "tok-2", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListWorkspaces(ctx)
	if err != nil || len(list) != 2 || list[0].Prefix != "aaa" || list[1].Prefix != "acme" {
		t.Errorf("ListWorkspaces = %+v (err %v)", list, err)
	}

	// Update (rotation, credential clearing) and stale-token lookups.
	w.Token = "ws-tok-2"
	w.DefaultBranch = "trunk"
	w.ForgeCredentials = nil
	w.GitHubAppBroken = true
	if err := st.UpdateWorkspace(ctx, w); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WorkspaceByToken(ctx, "ws-tok"); !errors.Is(err, store.ErrNotFound) {
		t.Error("old token still resolves")
	}
	got, err := st.WorkspaceByPrefix(ctx, "acme")
	if err != nil || got.Token != "ws-tok-2" || got.DefaultBranch != "trunk" {
		t.Errorf("after update: %+v (err %v)", got, err)
	}
	if got.ForgeCredentials != nil {
		t.Errorf("credentials not cleared: %v", got.ForgeCredentials)
	}
	if !got.GitHubAppBroken {
		t.Error("broken flag not persisted")
	}

	// Delete; missing rows yield ErrNotFound.
	if err := st.DeleteWorkspace(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteWorkspace(ctx, w.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete = %v", err)
	}
	if err := st.UpdateWorkspace(ctx, &store.Workspace{ID: 9999, Prefix: "x", Token: "y"}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("update missing = %v", err)
	}
}

func TestUploadLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	repo := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "tok", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	blocks := []profile.Block{
		{StartLine: 1, StartCol: 1, EndLine: 5, EndCol: 2, NumStmts: 3, Count: 1},
		{StartLine: 7, StartCol: 1, EndLine: 9, EndCol: 2, NumStmts: 2, Count: 0},
	}
	mkUpload := func(commit, branch string, pct float64, dc *diffcov.Result) *store.Upload {
		t.Helper()
		u := &store.Upload{
			RepoID: repo.ID, CommitSHA: commit, Branch: branch, Format: "go",
			TotalPct: pct, CoveredStmts: 3, TotalStmts: 5,
			RawBlobKey: "profiles/" + commit, DiffCoverage: dc,
		}
		files := []*store.UploadFile{
			{Path: "example.com/m/b.go", Pct: 100, CoveredStmts: 2, TotalStmts: 2, Blocks: blocks[:1]},
			{Path: "example.com/m/a.go", Pct: 60, CoveredStmts: 3, TotalStmts: 5, Blocks: blocks},
		}
		if err := st.CreateUpload(ctx, u, files); err != nil {
			t.Fatal(err)
		}
		return u
	}

	u1 := mkUpload("c1", "main", 60, nil)
	u2 := mkUpload("c2", "main", 65, nil)
	dc := &diffcov.Result{
		Files: []diffcov.FileCoverage{
			{Path: "m/a.go", CoveredLines: 2, TotalLines: 3, UncoveredLines: []int{9}},
		},
		CoveredLines: 2, TotalLines: 3,
		UnmatchedFiles: []string{"m/new.go"},
	}
	u3 := mkUpload("c3", "feature/x", 70, dc)

	// Full round trip of a stored upload.
	got, err := st.Upload(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CommitSHA != "c1" || got.Branch != "main" || got.TotalPct != 60 ||
		got.RawBlobKey != "profiles/c1" || got.DiffCoverage != nil {
		t.Errorf("upload round trip: %+v", got)
	}

	// Per-file rows are sorted and preserve block data exactly.
	files, err := st.UploadFiles(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "example.com/m/a.go" {
		t.Fatalf("files = %+v", files)
	}
	if !reflect.DeepEqual(files[0].Blocks, blocks) {
		t.Errorf("blocks round trip: %+v", files[0].Blocks)
	}

	// Diff coverage round-trips through JSONB.
	got3, err := st.Upload(ctx, u3.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got3.DiffCoverage, dc) {
		t.Errorf("diff coverage round trip:\n got %+v\nwant %+v", got3.DiffCoverage, dc)
	}

	// ListUploads: newest first, limited and unlimited.
	ups, err := st.ListUploads(ctx, repo.ID, 2)
	if err != nil || len(ups) != 2 || ups[0].ID != u3.ID || ups[1].ID != u2.ID {
		t.Errorf("limited list = %v (err %v)", ups, err)
	}
	ups, err = st.ListUploads(ctx, repo.ID, 0)
	if err != nil || len(ups) != 3 {
		t.Errorf("unlimited list = %d uploads (err %v)", len(ups), err)
	}

	// The gate_failed flag round-trips (the per-upload web views read it).
	failed := &store.Upload{
		RepoID: repo.ID, CommitSHA: "c4", Branch: "main", Format: "go",
		TotalPct: 10, CoveredStmts: 1, TotalStmts: 10, GateFailed: true,
	}
	if err := st.CreateUpload(ctx, failed, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Upload(ctx, failed.ID); err != nil || !got.GateFailed {
		t.Errorf("gate_failed round trip: %+v (err %v)", got, err)
	}

	// DeleteRepo cascades to uploads and files.
	if err := st.DeleteRepo(ctx, repo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Upload(ctx, u1.ID); !errors.Is(err, store.ErrNotFound) {
		t.Error("upload survived repo deletion")
	}
	if _, err := st.RepoByID(ctx, repo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Error("repo survived deletion")
	}
	if err := st.DeleteRepo(ctx, repo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

func TestCommitReportLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	repo := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "tok", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	mkUpload := func(commit, part string, count int) *store.Upload {
		t.Helper()
		u := &store.Upload{RepoID: repo.ID, CommitSHA: commit, Branch: "main", Format: "go", Part: part}
		files := []*store.UploadFile{{Path: "a.go", Blocks: []profile.Block{
			{StartLine: 1, EndLine: 2, NumStmts: 1, Count: count},
		}}}
		if err := st.CreateUpload(ctx, u, files); err != nil {
			t.Fatal(err)
		}
		return u
	}

	// Two parts, plus a re-upload of one part that must supersede its
	// predecessor in LatestUploadsPerPart.
	mkUpload("c1", "backend", 0)
	be2 := mkUpload("c1", "backend", 1)
	fe := mkUpload("c1", "frontend", 1)
	parts, err := st.LatestUploadsPerPart(ctx, repo.ID, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Fatalf("latest per part = %d uploads, want 2 (backend superseded)", len(parts))
	}
	got := map[string]int64{}
	for _, p := range parts {
		got[p.Part] = p.ID
	}
	if got["backend"] != be2.ID || got["frontend"] != fe.ID {
		t.Errorf("latest per part = %v, want newest backend %d and frontend %d", got, be2.ID, fe.ID)
	}

	// Upsert creates, then replaces in place preserving id and created_at.
	dc := &diffcov.Result{Files: []diffcov.FileCoverage{{Path: "a.go", CoveredLines: 1, TotalLines: 2, UncoveredLines: []int{9}}}, CoveredLines: 1, TotalLines: 2}
	cr := &store.CommitReport{RepoID: repo.ID, CommitSHA: "c1", Branch: "main", PRID: "7",
		TotalPct: 50, CoveredStmts: 1, TotalStmts: 2, DiffCoverage: dc, PartCount: 2}
	if err := st.UpsertCommitReport(ctx, cr); err != nil {
		t.Fatal(err)
	}
	if cr.ID == 0 || cr.CreatedAt.IsZero() {
		t.Fatalf("upsert did not fill ID/CreatedAt: %+v", cr)
	}
	firstID, firstCreated := cr.ID, cr.CreatedAt
	cr2 := &store.CommitReport{RepoID: repo.ID, CommitSHA: "c1", Branch: "main",
		TotalPct: 80, CoveredStmts: 8, TotalStmts: 10, PartCount: 2}
	if err := st.UpsertCommitReport(ctx, cr2); err != nil {
		t.Fatal(err)
	}
	if cr2.ID != firstID || !cr2.CreatedAt.Equal(firstCreated) {
		t.Errorf("upsert changed id/created_at: id %d→%d", firstID, cr2.ID)
	}

	// Round trip: latest values win, diff_coverage cleared to nil.
	round, err := st.CommitReport(ctx, repo.ID, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if round.TotalPct != 80 || round.CoveredStmts != 8 || round.DiffCoverage != nil {
		t.Errorf("round trip = %+v, want 80%% 8/10 nil diff", round)
	}

	// Baseline selection: a passing report on another commit, skipping the
	// excluded commit and gate-failing reports.
	if err := st.UpsertCommitReport(ctx, &store.CommitReport{RepoID: repo.ID, CommitSHA: "c2", Branch: "main", TotalPct: 90, PartCount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertCommitReport(ctx, &store.CommitReport{RepoID: repo.ID, CommitSHA: "c3", Branch: "main", TotalPct: 10, GateFailed: true, PartCount: 1}); err != nil {
		t.Fatal(err)
	}
	if latest, err := st.LatestCommitReport(ctx, repo.ID, "main"); err != nil || latest.CommitSHA != "c3" {
		t.Errorf("latest report = %v, %v (want c3, the newest)", latest, err)
	}
	// Excluding c2 and skipping the failed c3 leaves c1 as the baseline.
	base, err := st.LatestPassedCommitReport(ctx, repo.ID, "main", "c2")
	if err != nil || base.CommitSHA != "c1" {
		t.Errorf("passed baseline excluding c2 = %v, %v (want c1)", base, err)
	}
	// The trend lists reports newest first.
	list, err := st.ListBranchCommitReports(ctx, repo.ID, "main", 0)
	if err != nil || len(list) != 3 || list[0].CommitSHA != "c3" {
		t.Errorf("branch reports = %+v (err %v)", list, err)
	}

	// DeleteRepo cascades to commit reports.
	if err := st.DeleteRepo(ctx, repo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CommitReport(ctx, repo.ID, "c1"); !errors.Is(err, store.ErrNotFound) {
		t.Error("commit report survived repo deletion")
	}
}

func TestWithCommitReportTx(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	repo := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "tok", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	// Concurrency stress: far more simultaneous recomputes of one commit than
	// the pool has connections, each issuing a read and a write *inside* the
	// lock. The previous design held one pooled connection for the advisory
	// lock and reached for a second to run these queries, which deadlocked
	// the pool here; routing the queries through the locked transaction's own
	// connection must not.
	const n = 32
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			errs[i] = st.WithCommitReportTx(cctx, repo.ID, "c1", func(ctx context.Context, tx store.CommitTx) error {
				if _, err := tx.LatestUploadsPerPart(ctx, repo.ID, "c1"); err != nil {
					return err
				}
				return tx.UpsertCommitReport(ctx, &store.CommitReport{
					RepoID: repo.ID, CommitSHA: "c1", Branch: "main", TotalPct: float64(i), PartCount: 1,
				})
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("tx %d failed (a pool deadlock would surface here as a timeout): %v", i, err)
		}
	}
	if cr, err := st.CommitReport(ctx, repo.ID, "c1"); err != nil || cr.PartCount != 1 {
		t.Fatalf("commit report after %d concurrent txs = %+v, %v (want one serialized row)", n, cr, err)
	}

	// Serialization: a second recompute of the same commit waits for the
	// first to finish.
	release := make(chan struct{})
	entered := make(chan struct{})
	go func() {
		_ = st.WithCommitReportTx(ctx, repo.ID, "c1", func(context.Context, store.CommitTx) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	second := make(chan struct{})
	go func() {
		_ = st.WithCommitReportTx(ctx, repo.ID, "c1", func(context.Context, store.CommitTx) error { return nil })
		close(second)
	}()
	select {
	case <-second:
		t.Error("second recompute ran while the first held the commit lock")
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	select {
	case <-second:
	case <-time.After(3 * time.Second):
		t.Error("second recompute never ran after the first released")
	}

	// A different commit never blocks behind a held one.
	done := make(chan error, 1)
	blocker := make(chan struct{})
	go func() {
		done <- st.WithCommitReportTx(ctx, repo.ID, "c1", func(context.Context, store.CommitTx) error {
			<-blocker
			return nil
		})
	}()
	if err := st.WithCommitReportTx(ctx, repo.ID, "c2", func(context.Context, store.CommitTx) error { return nil }); err != nil {
		t.Errorf("a different commit blocked behind a held lock: %v", err)
	}
	close(blocker)
	if err := <-done; err != nil {
		t.Errorf("c1 tx: %v", err)
	}
}

// backfillCommitReportsSQL mirrors the backfill statement in
// 0012_commit_reports.sql. store.CreateUpload never writes commit_reports
// (only the server's recompute does), so inserting uploads and running this
// reproduces the migration's effect on a repo that predates the feature.
const backfillCommitReportsSQL = `
INSERT INTO commit_reports (repo_id, commit_sha, branch, pr_id, total_pct,
    covered_stmts, total_stmts, gate_failed, diff_coverage, part_count, created_at, updated_at)
SELECT repo_id, commit_sha, branch, pr_id, total_pct, covered_stmts, total_stmts,
       gate_failed, diff_coverage, 1, created_at, created_at
FROM (SELECT DISTINCT ON (repo_id, commit_sha) * FROM uploads
      ORDER BY repo_id, commit_sha, id DESC) latest
ORDER BY latest.id`

func TestCommitReportBackfill(t *testing.T) {
	pool := testpg.Pool(t)
	st := postgres.New(pool)
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "tok", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	mk := func(commit, branch string, pct float64) *store.Upload {
		u := &store.Upload{RepoID: repo.ID, CommitSHA: commit, Branch: branch, Format: "go", TotalPct: pct, CoveredStmts: 1, TotalStmts: 2}
		if err := st.CreateUpload(ctx, u, nil); err != nil {
			t.Fatal(err)
		}
		return u
	}
	// c1 uploaded twice (the later, higher-coverage row must win); c2 once.
	mk("c1", "main", 50)
	mk("c1", "main", 80)
	mk("c2", "main", 90)

	if _, err := pool.Exec(ctx, backfillCommitReportsSQL); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	cr1, err := st.CommitReport(ctx, repo.ID, "c1")
	if err != nil || cr1.TotalPct != 80 || cr1.PartCount != 1 {
		t.Fatalf("c1 report = %+v, %v (want latest 80%%, one part)", cr1, err)
	}
	cr2, err := st.CommitReport(ctx, repo.ID, "c2")
	if err != nil || cr2.TotalPct != 90 {
		t.Fatalf("c2 report = %+v, %v", cr2, err)
	}
	// id ascends with commit order (c1's latest upload precedes c2's), so the
	// branch's newest report is c2 — what badge/trend read.
	if cr2.ID <= cr1.ID {
		t.Errorf("backfill ids out of order: c1=%d c2=%d", cr1.ID, cr2.ID)
	}
	if latest, err := st.LatestCommitReport(ctx, repo.ID, "main"); err != nil || latest.CommitSHA != "c2" {
		t.Errorf("latest report = %v, %v (want c2)", latest, err)
	}
}

func TestClaimStatusPush(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	repo := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "tok", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	// Nothing to claim before the report exists.
	if ok, err := st.ClaimStatusPush(ctx, repo.ID, "c1", 5); err != nil || ok {
		t.Fatalf("claim on missing report = %v, %v (want false)", ok, err)
	}
	if err := st.UpsertCommitReport(ctx, &store.CommitReport{RepoID: repo.ID, CommitSHA: "c1", Branch: "main", PartCount: 1}); err != nil {
		t.Fatal(err)
	}

	// A version only claims if strictly greater than the last claimed.
	steps := []struct {
		version int64
		want    bool
	}{
		{5, true},  // first claim
		{5, false}, // equal — already pushed
		{3, false}, // older push arriving late — must lose
		{7, true},  // newer recompute wins
	}
	for _, s := range steps {
		if ok, err := st.ClaimStatusPush(ctx, repo.ID, "c1", s.version); err != nil || ok != s.want {
			t.Errorf("claim v%d = %v (want %v), err %v", s.version, ok, s.want, err)
		}
	}
}

func TestUserLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	u := &store.User{Forge: "bitbucket", ForgeUUID: "{u1}", Email: "jane@example.com", DisplayName: "Jane Dev",
		ForgeWorkspaces: []string{"acme", "personal"}}
	if err := st.UpsertUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 || u.CreatedAt.IsZero() || u.LastLoginAt.IsZero() {
		t.Fatalf("UpsertUser did not fill ID/CreatedAt/LastLoginAt: %+v", u)
	}

	// A second login by the same forge account refreshes the same row (R1),
	// including the forge workspace snapshot (M3/D3).
	again := &store.User{Forge: "bitbucket", ForgeUUID: "{u1}", Email: "jane@new.example", DisplayName: "Jane Renamed",
		ForgeWorkspaces: []string{"acme", "newco"}}
	if err := st.UpsertUser(ctx, again); err != nil {
		t.Fatal(err)
	}
	if again.ID != u.ID {
		t.Errorf("second login created a new row: %d != %d", again.ID, u.ID)
	}
	got, err := st.UserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "jane@new.example" || got.DisplayName != "Jane Renamed" {
		t.Errorf("fields not refreshed: %+v", got)
	}
	if !reflect.DeepEqual(got.ForgeWorkspaces, []string{"acme", "newco"}) {
		t.Errorf("forge workspaces not refreshed: %v", got.ForgeWorkspaces)
	}
	if got.LastLoginAt.Before(u.LastLoginAt) {
		t.Errorf("last_login_at went backwards: %v < %v", got.LastLoginAt, u.LastLoginAt)
	}

	// Same UUID under another forge must not collide (R1).
	other := &store.User{Forge: "github", ForgeUUID: "{u1}", Email: "x@example.com", DisplayName: "X"}
	if err := st.UpsertUser(ctx, other); err != nil {
		t.Fatal(err)
	}
	if other.ID == u.ID {
		t.Error("uniqueness must be per forge+UUID, not per UUID")
	}

	users, err := st.ListUsers(ctx)
	if err != nil || len(users) != 2 {
		t.Fatalf("ListUsers = %d users, %v", len(users), err)
	}

	if err := st.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UserByID(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Error("user survived deletion")
	}
	if err := st.DeleteUser(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	u := &store.User{Forge: "bitbucket", ForgeUUID: "{s1}", Email: "s@example.com", DisplayName: "S"}
	if err := st.UpsertUser(ctx, u); err != nil {
		t.Fatal(err)
	}

	sess := &store.Session{TokenHash: "hash-live", UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := st.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if sess.CreatedAt.IsZero() {
		t.Error("CreateSession did not fill CreatedAt")
	}
	got, err := st.UserBySession(ctx, "hash-live")
	if err != nil || got.ID != u.ID {
		t.Fatalf("UserBySession = %v, %v", got, err)
	}

	// R2: a session past expiry never authenticates.
	expired := &store.Session{TokenHash: "hash-expired", UserID: u.ID, ExpiresAt: time.Now().Add(-time.Minute)}
	if err := st.CreateSession(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UserBySession(ctx, "hash-expired"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expired session authenticated: %v", err)
	}

	// R2: logout invalidates server-side immediately.
	if err := st.DeleteSession(ctx, "hash-live"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UserBySession(ctx, "hash-live"); !errors.Is(err, store.ErrNotFound) {
		t.Error("deleted session still authenticates")
	}
	if err := st.DeleteSession(ctx, "hash-live"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}

	// R2: deleting a user deletes their sessions.
	sess2 := &store.Session{TokenHash: "hash-cascade", UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := st.CreateSession(ctx, sess2); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UserBySession(ctx, "hash-cascade"); !errors.Is(err, store.ErrNotFound) {
		t.Error("session survived user deletion")
	}
}

func TestWorkspaceMembership(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	u := &store.User{Forge: "bitbucket", ForgeUUID: "{m1}", DisplayName: "Member"}
	if err := st.UpsertUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	acme := &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "tok-acme"}
	beta := &store.Workspace{Forge: "bitbucket", Prefix: "beta", Token: "tok-beta"}
	for _, w := range []*store.Workspace{acme, beta} {
		if err := st.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
	}

	prefixes := func() []string {
		t.Helper()
		wss, err := st.ListWorkspacesForUser(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(wss))
		for i, w := range wss {
			out[i] = w.Prefix
		}
		return out
	}
	memberRows := func() int {
		t.Helper()
		var n int
		if err := st.Pool().QueryRow(ctx,
			`SELECT count(*) FROM workspace_members WHERE user_id = $1`, u.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Initial sync attaches both, ordered by prefix.
	if err := st.SetUserWorkspaces(ctx, u.ID, []int64{beta.ID, acme.ID}); err != nil {
		t.Fatal(err)
	}
	if got := prefixes(); !reflect.DeepEqual(got, []string{"acme", "beta"}) {
		t.Fatalf("after sync: %v, want [acme beta]", got)
	}

	// Re-running with the same set is idempotent (no duplicate rows).
	if err := st.SetUserWorkspaces(ctx, u.ID, []int64{acme.ID, beta.ID}); err != nil {
		t.Fatal(err)
	}
	if n := memberRows(); n != 2 {
		t.Fatalf("idempotent re-sync produced %d rows, want 2", n)
	}

	// Dropping beta on the forge removes only that membership.
	if err := st.SetUserWorkspaces(ctx, u.ID, []int64{acme.ID}); err != nil {
		t.Fatal(err)
	}
	if got := prefixes(); !reflect.DeepEqual(got, []string{"acme"}) {
		t.Fatalf("after drop: %v, want [acme]", got)
	}

	// Deleting the workspace cascades the membership away.
	if err := st.DeleteWorkspace(ctx, acme.ID); err != nil {
		t.Fatal(err)
	}
	if got := prefixes(); len(got) != 0 {
		t.Fatalf("membership survived workspace deletion: %v", got)
	}

	// Re-attach, then delete the user: memberships cascade on that side too.
	if err := st.SetUserWorkspaces(ctx, u.ID, []int64{beta.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if n := memberRows(); n != 0 {
		t.Fatalf("membership survived user deletion: %d rows", n)
	}

	// An empty sync clears everything and is safe on a user with no rows.
	other := &store.User{Forge: "bitbucket", ForgeUUID: "{m2}", DisplayName: "Other"}
	if err := st.UpsertUser(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserWorkspaces(ctx, other.ID, nil); err != nil {
		t.Fatalf("empty sync on fresh user: %v", err)
	}
}

func TestRegisterWorkspace(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	u := &store.User{Forge: "bitbucket", ForgeUUID: "{r1}", DisplayName: "Founder"}
	if err := st.UpsertUser(ctx, u); err != nil {
		t.Fatal(err)
	}

	// Registration creates the workspace and its first membership together.
	w := &store.Workspace{Forge: "bitbucket", Prefix: "startup", Token: "reg-tok", DefaultBranch: "main"}
	if err := st.RegisterWorkspace(ctx, w, u.ID); err != nil {
		t.Fatal(err)
	}
	if w.ID == 0 || w.CreatedAt.IsZero() {
		t.Fatalf("RegisterWorkspace did not fill ID/CreatedAt: %+v", w)
	}
	wss, err := st.ListWorkspacesForUser(ctx, u.ID)
	if err != nil || len(wss) != 1 || wss[0].Prefix != "startup" {
		t.Fatalf("memberships after registration = %v, %v", wss, err)
	}

	// A losing duplicate claim fails atomically: no workspace row, no
	// membership row, and the winner's registration is untouched.
	other := &store.User{Forge: "bitbucket", ForgeUUID: "{r2}", DisplayName: "Latecomer"}
	if err := st.UpsertUser(ctx, other); err != nil {
		t.Fatal(err)
	}
	dup := &store.Workspace{Forge: "bitbucket", Prefix: "startup", Token: "other-tok", DefaultBranch: "main"}
	if err := st.RegisterWorkspace(ctx, dup, other.ID); err == nil {
		t.Fatal("duplicate registration must fail")
	}
	if wss, _ := st.ListWorkspacesForUser(ctx, other.ID); len(wss) != 0 {
		t.Errorf("failed registration left memberships: %v", wss)
	}
	if got, err := st.WorkspaceByPrefix(ctx, "startup"); err != nil || got.Token != "reg-tok" {
		t.Errorf("winner's workspace disturbed: %+v (err %v)", got, err)
	}
}

func TestBitbucketGrantEncryptedAtRest(t *testing.T) {
	st := newTestStore(t)
	box, err := secretbox.New("test-secret-key")
	if err != nil {
		t.Fatal(err)
	}
	st.SetCipher(box)
	ctx := context.Background()

	w := &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "ws-tok", DefaultBranch: "main"}
	if err := st.CreateWorkspace(ctx, w); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspaceBitbucketGrant(ctx, w.ID, "covbot", "rt-secret-1", false); err != nil {
		t.Fatal(err)
	}

	// The column never sees the plaintext.
	var raw string
	if err := st.Pool().QueryRow(ctx,
		`SELECT bitbucket_refresh_token FROM workspaces WHERE id = $1`, w.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "v1:") || strings.Contains(raw, "rt-secret-1") {
		t.Errorf("stored column = %q, want sealed v1: value", raw)
	}

	got, err := st.WorkspaceByPrefix(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if got.BitbucketRefreshToken != "rt-secret-1" || got.BitbucketGrantAccount != "covbot" || got.BitbucketGrantBroken {
		t.Errorf("loaded grant = %q/%q/%v", got.BitbucketGrantAccount, got.BitbucketRefreshToken, got.BitbucketGrantBroken)
	}

	// Rotation: the swap replaces the stored token.
	if err := st.SetWorkspaceBitbucketGrant(ctx, w.ID, "covbot", "rt-secret-2", false); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.WorkspaceByPrefix(ctx, "acme"); got.BitbucketRefreshToken != "rt-secret-2" {
		t.Errorf("after rotation: %q, want rt-secret-2", got.BitbucketRefreshToken)
	}

	// UpdateWorkspace must not touch the grant columns — a full-row
	// write from an earlier read would resurrect a rotated-away token.
	stale := *got
	stale.BitbucketRefreshToken = "rt-secret-1"
	stale.DefaultBranch = "trunk"
	if err := st.UpdateWorkspace(ctx, &stale); err != nil {
		t.Fatal(err)
	}
	got, _ = st.WorkspaceByPrefix(ctx, "acme")
	if got.DefaultBranch != "trunk" || got.BitbucketRefreshToken != "rt-secret-2" {
		t.Errorf("after full-row update: branch %q token %q, want trunk + untouched rt-secret-2",
			got.DefaultBranch, got.BitbucketRefreshToken)
	}

	// A different (rotated-away) key cannot brick reads: the token comes
	// back empty and the connection reads as broken -> reconnect.
	otherBox, _ := secretbox.New("some-other-key")
	st2 := postgres.New(st.Pool())
	st2.SetCipher(otherBox)
	got, err = st2.WorkspaceByPrefix(ctx, "acme")
	if err != nil {
		t.Fatalf("wrong key must degrade, not error: %v", err)
	}
	if got.BitbucketRefreshToken != "" || !got.BitbucketGrantBroken {
		t.Errorf("wrong key: token %q broken %v, want empty + broken", got.BitbucketRefreshToken, got.BitbucketGrantBroken)
	}

	// Writing a grant without a cipher fails loudly.
	st3 := postgres.New(st.Pool())
	if err := st3.SetWorkspaceBitbucketGrant(ctx, w.ID, "covbot", "rt-plain", false); err == nil {
		t.Error("storing a grant without GOCOV_SECRET_KEY must fail")
	}
	// Clearing the grant needs no cipher (empty token).
	if err := st3.SetWorkspaceBitbucketGrant(ctx, w.ID, "", "", false); err != nil {
		t.Errorf("clearing without cipher: %v", err)
	}
}
