package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/secretbox"
	"github.com/gocov/gocov/internal/store"
	"github.com/gocov/gocov/internal/store/postgres"
	"github.com/gocov/gocov/internal/store/storetest"
	"github.com/gocov/gocov/internal/testpg"
)

func newTestStore(t *testing.T) *postgres.Store {
	t.Helper()
	st := postgres.New(testpg.Pool(t))
	ctx := t.Context()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Migrations must be idempotent across restarts.
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	return st
}

func TestWithCommitReportTx(t *testing.T) {
	st := newTestStore(t)
	ctx := t.Context()

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
	for i := range n {
		wg.Go(func() {
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
		})
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

func TestCommitReportBackfill(t *testing.T) {
	pool := testpg.Pool(t)
	st := postgres.New(pool)
	ctx := t.Context()
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
	// store.CreateUpload never writes commit_reports (only the recompute
	// does), so these stand in for a repo that predates the feature.
	mk("c1", "main", 50)
	c1Latest := mk("c1", "main", 80)
	c2 := mk("c2", "main", 90)

	// Run the real backfill migration file, not a copy that could drift from
	// it — this is what a deploy executes. It is ON CONFLICT DO NOTHING, so
	// re-running after Migrate already applied it on the empty table is safe.
	sql, err := os.ReadFile("migrations/0013_backfill_commit_reports.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	cr1, err := st.CommitReport(ctx, repo.ID, "c1")
	if err != nil || cr1.TotalPct != 80 || cr1.PartCount != 1 {
		t.Fatalf("c1 report = %+v, %v (want latest 80%%, one part)", cr1, err)
	}
	// upload_id must be backfilled (the trend links through it) — the exact
	// column an out-of-date hand-copied statement would have missed.
	if cr1.UploadID != c1Latest.ID {
		t.Errorf("c1 upload_id = %d, want %d (latest upload)", cr1.UploadID, c1Latest.ID)
	}
	cr2, err := st.CommitReport(ctx, repo.ID, "c2")
	if err != nil || cr2.TotalPct != 90 || cr2.UploadID != c2.ID {
		t.Fatalf("c2 report = %+v, %v", cr2, err)
	}
	// id ascends with commit order (c1's latest upload precedes c2's), so the
	// branch's newest report is c2 — what badge/trend read.
	if cr2.ID <= cr1.ID {
		t.Errorf("backfill ids out of order: c1=%d c2=%d", cr1.ID, cr2.ID)
	}
	if latest, err := st.DefaultBranchReports(ctx, []int64{repo.ID}, 1); err != nil || len(latest[repo.ID]) != 1 || latest[repo.ID][0].CommitSHA != "c2" {
		t.Errorf("latest report = %v, %v (want c2)", latest, err)
	}
}

func TestWithGrantLock(t *testing.T) {
	st := newTestStore(t)
	box, err := secretbox.New(testSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	st.SetCipher(box)
	ctx := t.Context()

	ws := &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "tok", DefaultBranch: "main"}
	if err := st.CreateWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspaceGrant(ctx, ws.ID, "bitbucket", store.Grant{Account: "covbot", RefreshToken: "rt-0"}); err != nil {
		t.Fatal(err)
	}

	// The same stress as TestWithCommitReportTx: far more simultaneous
	// refreshes of one grant than the pool has connections, each reading
	// the stored token and rotating it *inside* the lock, which must run
	// on the locked transaction's own connection or the pool deadlocks.
	// Serialization shows in the result: every refresh sees the token
	// the previous one stored, so the chain rt-0 → rt-1 → … never skips.
	const n = 32
	var wg sync.WaitGroup
	errs := make([]error, n)
	var rotations atomic.Int64
	for i := range n {
		wg.Go(func() {
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			errs[i] = st.WithGrantLock(cctx, ws.ID, func(ctx context.Context, tx store.GrantTx) error {
				fresh, err := tx.WorkspaceByPrefix(ctx, "bitbucket", "acme")
				if err != nil {
					return err
				}
				var seq int
				if _, err := fmt.Sscanf(fresh.Grant.RefreshToken, "rt-%d", &seq); err != nil {
					return fmt.Errorf("stored token %q: %w", fresh.Grant.RefreshToken, err)
				}
				if got := rotations.Load(); int64(seq) != got {
					return fmt.Errorf("read rt-%d under the lock, but %d rotations have happened", seq, got)
				}
				rotations.Add(1)
				return tx.SetWorkspaceGrant(ctx, ws.ID, "bitbucket", store.Grant{Account: "covbot", RefreshToken: fmt.Sprintf("rt-%d", seq+1)})
			})
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("refresh %d: %v", i, err)
		}
	}
	fresh, err := st.WorkspaceByPrefix(ctx, "bitbucket", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("rt-%d", n); fresh.Grant.RefreshToken != want {
		t.Errorf("stored token = %q, want %q after %d serialized rotations", fresh.Grant.RefreshToken, want, n)
	}

	// The lock is transaction-scoped: fn's error rolls back and releases
	// it, so the next holder is not stuck behind a failed refresh.
	boom := errors.New("forge is down")
	if err := st.WithGrantLock(ctx, ws.ID, func(context.Context, store.GrantTx) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := st.WithGrantLock(lctx, ws.ID, func(context.Context, store.GrantTx) error { return nil }); err != nil {
		t.Fatalf("lock not released after a failed fn: %v", err)
	}
}

func TestMembershipRolesGrandfatherExistingSeats(t *testing.T) {
	// Before roles existed every member had full rights, so the migration
	// promotes the seats it finds rather than silently demoting them; the
	// next login replaces each with the forge's answer.
	pool := testpg.Pool(t)
	st := postgres.New(pool)
	ctx := t.Context()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u := &store.User{Forge: "bitbucket", ForgeUUID: "{g1}", DisplayName: "Early Adopter"}
	if err := st.UpsertUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	ws := &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "tok-acme"}
	if err := st.CreateWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	// A pre-roles seat: the row as the previous schema wrote it, which the
	// column's default now reads as a plain member.
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id) VALUES ($1, $2)`, ws.ID, u.ID); err != nil {
		t.Fatal(err)
	}

	// Run the real migration file, not a copy that could drift from it —
	// this is what a deploy executes. Its ALTERs are IF NOT EXISTS, so
	// re-running after Migrate already applied it is safe.
	sql, err := os.ReadFile("migrations/0022_membership_roles.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("migration: %v", err)
	}

	ms, err := st.ListMembershipsForUser(ctx, u.ID)
	if err != nil || !reflect.DeepEqual(ms, []store.Membership{{WorkspaceID: ws.ID, Role: store.RoleOwner}}) {
		t.Fatalf("grandfathered seat = %+v, %v (want owner)", ms, err)
	}
}

// secretbox.New takes the key as 64 hex characters, so the grant tests
// use fixed ones rather than a memorable string.
const (
	testSecretKey  = "4b1d0f8a2c6e59d3a7f014b8e2c95d36a8b7c40e1f2a3b4c5d6e7f8091a2b3c4"
	otherSecretKey = "9f3e2d1c0b9a8776655443322110ffeeddccbbaa99887766554433221100aabb"
)

func TestBitbucketGrantEncryptedAtRest(t *testing.T) {
	st := newTestStore(t)
	box, err := secretbox.New(testSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	st.SetCipher(box)
	ctx := t.Context()

	w := &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "ws-tok", DefaultBranch: "main"}
	if err := st.CreateWorkspace(ctx, w); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspaceGrant(ctx, w.ID, "bitbucket", store.Grant{Account: "covbot", RefreshToken: "rt-secret-1"}); err != nil {
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

	got, err := st.WorkspaceByPrefix(ctx, "bitbucket", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if got.Grant.RefreshToken != "rt-secret-1" || got.Grant.Account != "covbot" || got.Grant.Broken {
		t.Errorf("loaded grant = %q/%q/%v", got.Grant.Account, got.Grant.RefreshToken, got.Grant.Broken)
	}

	// Rotation: the swap replaces the stored token.
	if err := st.SetWorkspaceGrant(ctx, w.ID, "bitbucket", store.Grant{Account: "covbot", RefreshToken: "rt-secret-2"}); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.WorkspaceByPrefix(ctx, "bitbucket", "acme"); got.Grant.RefreshToken != "rt-secret-2" {
		t.Errorf("after rotation: %q, want rt-secret-2", got.Grant.RefreshToken)
	}

	// UpdateWorkspace must not touch the grant columns — a full-row
	// write from an earlier read would resurrect a rotated-away token.
	stale := *got
	stale.Grant.RefreshToken = "rt-secret-1"
	stale.DefaultBranch = "trunk"
	if err := st.UpdateWorkspace(ctx, &stale); err != nil {
		t.Fatal(err)
	}
	got, _ = st.WorkspaceByPrefix(ctx, "bitbucket", "acme")
	if got.DefaultBranch != "trunk" || got.Grant.RefreshToken != "rt-secret-2" {
		t.Errorf("after full-row update: branch %q token %q, want trunk + untouched rt-secret-2",
			got.DefaultBranch, got.Grant.RefreshToken)
	}

	// A different (rotated-away) key cannot brick reads: the token comes
	// back empty and the connection reads as broken -> reconnect.
	otherBox, _ := secretbox.New(otherSecretKey)
	st2 := postgres.New(st.Pool())
	st2.SetCipher(otherBox)
	got, err = st2.WorkspaceByPrefix(ctx, "bitbucket", "acme")
	if err != nil {
		t.Fatalf("wrong key must degrade, not error: %v", err)
	}
	if got.Grant.RefreshToken != "" || !got.Grant.Broken {
		t.Errorf("wrong key: token %q broken %v, want empty + broken", got.Grant.RefreshToken, got.Grant.Broken)
	}

	// Writing a grant without a cipher fails loudly.
	st3 := postgres.New(st.Pool())
	if err := st3.SetWorkspaceGrant(ctx, w.ID, "bitbucket", store.Grant{Account: "covbot", RefreshToken: "rt-plain"}); err == nil {
		t.Error("storing a grant without GOCOV_SECRET_KEY must fail")
	}
	// Clearing the grant needs no cipher (empty token).
	if err := st3.SetWorkspaceGrant(ctx, w.ID, "bitbucket", store.Grant{}); err != nil {
		t.Errorf("clearing without cipher: %v", err)
	}
}

func TestGitLabGrantEncryptedAtRest(t *testing.T) {
	st := newTestStore(t)
	box, err := secretbox.New(testSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	st.SetCipher(box)
	ctx := t.Context()

	w := &store.Workspace{Forge: "gitlab", Prefix: "grp/sub", Token: "ws-tok", DefaultBranch: "main"}
	if err := st.CreateWorkspace(ctx, w); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspaceGrant(ctx, w.ID, "gitlab", store.Grant{Account: "covbot", RefreshToken: "rt-secret-1"}); err != nil {
		t.Fatal(err)
	}

	// The column never sees the plaintext.
	var raw string
	if err := st.Pool().QueryRow(ctx,
		`SELECT gitlab_refresh_token FROM workspaces WHERE id = $1`, w.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "v1:") || strings.Contains(raw, "rt-secret-1") {
		t.Errorf("stored column = %q, want sealed v1: value", raw)
	}

	got, err := st.WorkspaceByPrefix(ctx, "gitlab", "grp/sub")
	if err != nil {
		t.Fatal(err)
	}
	if got.Grant.RefreshToken != "rt-secret-1" || got.Grant.Account != "covbot" || got.Grant.Broken {
		t.Errorf("loaded grant = %q/%q/%v", got.Grant.Account, got.Grant.RefreshToken, got.Grant.Broken)
	}

	// Rotation swap + full-row-update isolation, in one pass.
	if err := st.SetWorkspaceGrant(ctx, w.ID, "gitlab", store.Grant{Account: "covbot", RefreshToken: "rt-secret-2"}); err != nil {
		t.Fatal(err)
	}
	stale := *got
	stale.Grant.RefreshToken = "rt-secret-1"
	stale.DefaultBranch = "trunk"
	if err := st.UpdateWorkspace(ctx, &stale); err != nil {
		t.Fatal(err)
	}
	got, _ = st.WorkspaceByPrefix(ctx, "gitlab", "grp/sub")
	if got.DefaultBranch != "trunk" || got.Grant.RefreshToken != "rt-secret-2" {
		t.Errorf("after rotation + full-row update: branch %q token %q, want trunk + rt-secret-2",
			got.DefaultBranch, got.Grant.RefreshToken)
	}
}

// A grant lives in its own forge's columns and only on a workspace of that
// forge: created with the row, it reads back; aimed at another forge, the
// write finds nothing to update.
func TestWorkspaceGrantStaysOnItsForge(t *testing.T) {
	st := newTestStore(t)
	box, err := secretbox.New(testSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	st.SetCipher(box)
	ctx := t.Context()

	w := &store.Workspace{Forge: "gitlab", Prefix: "acme", Token: "ws-tok", DefaultBranch: "main",
		Grant: store.Grant{Account: "covbot", RefreshToken: "rt-0"}}
	if err := st.CreateWorkspace(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, err := st.WorkspaceByPrefix(ctx, "gitlab", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if got.Grant != (store.Grant{Account: "covbot", RefreshToken: "rt-0"}) {
		t.Errorf("created grant = %+v, want covbot/rt-0", got.Grant)
	}

	for _, forge := range []string{"bitbucket", "github"} {
		if err := st.SetWorkspaceGrant(ctx, w.ID, forge, store.Grant{Account: "other"}); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("SetWorkspaceGrant(%s) on a gitlab workspace = %v, want ErrNotFound", forge, err)
		}
	}
	if got, _ := st.WorkspaceByPrefix(ctx, "gitlab", "acme"); got.Grant.Account != "covbot" {
		t.Errorf("grant after refused writes = %+v, want it untouched", got.Grant)
	}
}

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return newTestStore(t) })
}
