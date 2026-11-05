package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/forge"
	bbforge "github.com/gocov/gocov/internal/forge/bitbucket"
	forgefake "github.com/gocov/gocov/internal/forge/fake"
	"github.com/gocov/gocov/internal/forge/github"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// fakeBB is a Bitbucket connector whose refreshes are counted and whose
// answers are scripted, so the tests can see how often a live token costs
// a round trip and what happens when the grant is gone.
type fakeBB struct {
	client    forge.Forge
	refreshes int
	err       error
	ttl       time.Duration
}

func (f *fakeBB) AuthorizeURL(state, redirectURI string) string {
	return "https://bb.example/authorize"
}
func (f *fakeBB) Exchange(ctx context.Context, code, redirectURI string) (*bbforge.Grant, error) {
	return nil, errors.New("not used")
}
func (f *fakeBB) Refresh(ctx context.Context, refreshToken, redirectURI string) (*bbforge.Grant, error) {
	f.refreshes++
	if f.err != nil {
		return nil, f.err
	}
	ttl := f.ttl
	if ttl == 0 {
		ttl = time.Hour
	}
	// Bitbucket rotates the refresh token on every use.
	return &bbforge.Grant{Account: "covbot", AccessToken: "at-1", RefreshToken: "rt-next", TTL: ttl}, nil
}
func (f *fakeBB) ForgeClient(accessToken string) forge.Forge { return f.client }

func newForges(t *testing.T, bb GrantConnect) (*Forges, *storemem.Store) {
	t.Helper()
	st := storemem.New()
	return NewForges(st, slog.New(slog.NewTextHandler(io.Discard, nil)), "https://cov.example.com", nil, map[string]GrantConnect{"bitbucket": bb}), st
}

func connectedWorkspace(t *testing.T, st *storemem.Store, prefix string) *store.Workspace {
	t.Helper()
	ctx := t.Context()
	ws := &store.Workspace{Forge: "bitbucket", Prefix: prefix, Token: "ws-tok", DefaultBranch: "main"}
	if err := st.CreateWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspaceGrant(ctx, ws.ID, "bitbucket", store.Grant{Account: "covbot", RefreshToken: "rt-0"}); err != nil {
		t.Fatal(err)
	}
	fresh, err := st.WorkspaceByPrefix(ctx, "bitbucket", prefix)
	if err != nil {
		t.Fatal(err)
	}
	return fresh
}

func TestGrantTokenIsCachedBetweenUploads(t *testing.T) {
	bb := &fakeBB{client: forgefake.New()}
	f, st := newForges(t, bb)
	ws := connectedWorkspace(t, st, "acme")
	ctx := t.Context()

	for i := range 3 {
		if fg := f.Connected(ctx, ws, "bitbucket"); fg == nil {
			t.Fatalf("call %d: no forge client", i)
		}
	}
	if bb.refreshes != 1 {
		t.Errorf("refreshes = %d, want 1: the access token should be cached", bb.refreshes)
	}

	// Disconnecting must not leave a usable token behind in memory.
	f.DropGrantToken("bitbucket", ws.ID)
	if fg := f.Connected(ctx, ws, "bitbucket"); fg == nil {
		t.Fatal("no client after the cache was dropped")
	}
	if bb.refreshes != 2 {
		t.Errorf("refreshes = %d, want 2 after dropping the cached token", bb.refreshes)
	}
}

func TestConcurrentUploadsShareOneRefresh(t *testing.T) {
	// A cold cache and a burst of uploads for one workspace: every
	// request misses the cache at once, and only the first to take the
	// grant lock may spend a refresh — the rest must find the token it
	// cached, not each rotate the refresh token in turn and hand out a
	// token minted from a stale one.
	bb := &fakeBB{client: forgefake.New()}
	f, st := newForges(t, bb)
	ws := connectedWorkspace(t, st, "acme")
	ctx := t.Context()

	const n = 16
	var wg sync.WaitGroup
	clients := make([]forge.Forge, n)
	for i := range n {
		wg.Go(func() { clients[i] = f.Connected(ctx, ws, "bitbucket") })
	}
	wg.Wait()
	for i, c := range clients {
		if c == nil {
			t.Errorf("upload %d: no forge client", i)
		}
	}
	if bb.refreshes != 1 {
		t.Errorf("refreshes = %d, want 1: the lock should let one refresh serve the burst", bb.refreshes)
	}
}

// lockWinnerStore stands in for a request that held the grant lock just
// before us: by the time the lock is ours, its token is in the cache.
type lockWinnerStore struct {
	*storemem.Store
	f *Forges
}

func (s lockWinnerStore) WithGrantLock(ctx context.Context, workspaceID int64, fn func(context.Context, store.GrantTx) error) error {
	s.f.CacheGrantToken("bitbucket", workspaceID, "at-winner", time.Hour)
	return s.Store.WithGrantLock(ctx, workspaceID, fn)
}

func TestCacheFilledWhileWaitingForLock(t *testing.T) {
	// The burst above takes this path only when the scheduler lets a
	// request miss the cache before the first refresh lands; this pins
	// it: a request that missed the cache and then waited out another's
	// refresh uses that token instead of spending a second one.
	bb := &fakeBB{client: forgefake.New()}
	f, st := newForges(t, bb)
	ws := connectedWorkspace(t, st, "acme")
	f.Store = lockWinnerStore{Store: st, f: f}

	token, err := f.accessToken(t.Context(), f.grants["bitbucket"], ws)
	if err != nil {
		t.Fatal(err)
	}
	if token != "at-winner" || bb.refreshes != 0 {
		t.Errorf("token = %q after %d refreshes, want the lock winner's at-winner and none", token, bb.refreshes)
	}
}

func TestRotatedRefreshTokenIsPersisted(t *testing.T) {
	// Every refresh invalidates the stored token, so losing the new one
	// breaks the next refresh: it has to reach the store before the
	// access token is handed out.
	bb := &fakeBB{client: forgefake.New()}
	f, st := newForges(t, bb)
	ws := connectedWorkspace(t, st, "acme")

	if fg := f.Connected(t.Context(), ws, "bitbucket"); fg == nil {
		t.Fatal("no forge client")
	}
	fresh, err := st.WorkspaceByPrefix(t.Context(), "bitbucket", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Grant.RefreshToken != "rt-next" {
		t.Errorf("stored refresh token = %q, want the rotated %q", fresh.Grant.RefreshToken, "rt-next")
	}
}

func TestRevokedGrantIsMarkedBroken(t *testing.T) {
	bb := &fakeBB{err: forge.ErrCredentialsRevoked}
	f, st := newForges(t, bb)
	ws := connectedWorkspace(t, st, "acme")
	ctx := t.Context()

	if fg := f.Connected(ctx, ws, "bitbucket"); fg != nil {
		t.Error("a revoked grant handed out a client")
	}
	fresh, err := st.WorkspaceByPrefix(ctx, "bitbucket", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.Grant.Broken {
		t.Error("the grant was not marked broken, so settings will not ask for a reconnect")
	}
	// The account is kept: it says who has to reconnect.
	if fresh.Grant.Account != "covbot" {
		t.Errorf("account = %q, want it kept", fresh.Grant.Account)
	}
}

func TestCapableFollowsTheConfiguredConnectors(t *testing.T) {
	f, _ := newForges(t, &fakeBB{})
	for forgeName, want := range map[string]bool{"bitbucket": true, "github": false, "gitlab": false, "gitea": false} {
		if got := f.Capable(forgeName); got != want {
			t.Errorf("Capable(%q) = %v, want %v", forgeName, got, want)
		}
	}
	bare, _ := newForges(t, nil)
	if bare.Capable("bitbucket") {
		t.Error("Capable = true with no connector configured")
	}
}

func TestWorkspaceForPrefersTheLongestPrefix(t *testing.T) {
	// A subgroup registered on its own must win over its parent: its
	// repos were onboarded with the subgroup's connection.
	f, st := newForges(t, nil)
	ctx := t.Context()
	for _, prefix := range []string{"acme", "acme/team"} {
		if err := st.CreateWorkspace(ctx, &store.Workspace{
			Forge: "gitlab", Prefix: prefix, Token: "tok-" + prefix, DefaultBranch: "main",
		}); err != nil {
			t.Fatal(err)
		}
	}
	got := f.WorkspaceFor(ctx, "acme/team/widgets", "gitlab")
	if got == nil || got.Prefix != "acme/team" {
		t.Fatalf("workspace = %v, want the subgroup acme/team", got)
	}
}

func TestWorkspaceForRefusesAnotherForge(t *testing.T) {
	// Prefixes are globally unique, so a workspace named "acme" on one
	// forge must never lend its grant to a same-named account on another.
	f, st := newForges(t, nil)
	ctx := t.Context()
	if err := st.CreateWorkspace(ctx, &store.Workspace{
		Forge: "gitlab", Prefix: "acme", Token: "tok", DefaultBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.WorkspaceFor(ctx, "acme/widgets", "github"); got != nil {
		t.Errorf("a github repo borrowed the gitlab workspace %q", got.Prefix)
	}
	if got := f.WorkspaceFor(ctx, "acme/widgets", "gitlab"); got == nil {
		t.Error("the gitlab repo did not find its own workspace")
	}
	if got := f.WorkspaceFor(ctx, "nobody/widgets", "gitlab"); got != nil {
		t.Errorf("got %q for an unregistered prefix", got.Prefix)
	}
}

func TestForWithoutAConnection(t *testing.T) {
	// No connection is not an error: the upload lands and the forge
	// surfaces report that they were skipped.
	f, _ := newForges(t, nil)
	if fg := f.For(t.Context(), &store.Repo{Forge: "bitbucket", Slug: "acme/widgets"}); fg != nil {
		t.Errorf("forge = %#v, want nil", fg)
	}
}

// A connection that breaks takes the public answers of its workspace's
// repos with it — nothing can re-verify them any more — but leaves repos
// a more specific workspace owns, and repos already private, alone.
func TestRevokedGrantClosesTheWorkspacesPublicRepos(t *testing.T) {
	bb := &fakeBB{err: forge.ErrCredentialsRevoked}
	f, st := newForges(t, bb)
	ws := connectedWorkspace(t, st, "acme")
	ctx := t.Context()
	if err := st.CreateWorkspace(ctx, &store.Workspace{Forge: "bitbucket", Prefix: "acme/sub", Token: "sub-tok", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	repos := map[string]string{"acme/widgets": store.VisibilityPublic, "acme/sub/proj": store.VisibilityPublic, "acme/secret": store.VisibilityPrivate}
	for slug, vis := range repos {
		r := &store.Repo{Forge: "bitbucket", Slug: slug, Token: "tok-" + slug, DefaultBranch: "main"}
		if err := st.CreateRepo(ctx, r); err != nil {
			t.Fatal(err)
		}
		if err := st.SetRepoVisibility(ctx, r.ID, vis, now); err != nil {
			t.Fatal(err)
		}
	}

	if fg := f.Connected(ctx, ws, "bitbucket"); fg != nil {
		t.Fatal("a revoked grant handed out a client")
	}
	for slug, want := range map[string]string{"acme/widgets": store.VisibilityPrivate, "acme/sub/proj": store.VisibilityPublic, "acme/secret": store.VisibilityPrivate} {
		got, err := st.RepoBySlug(ctx, "bitbucket", slug)
		if err != nil {
			t.Fatal(err)
		}
		if got.Visibility != want {
			t.Errorf("%s: visibility = %q, want %q", slug, got.Visibility, want)
		}
	}
}

// A connection that exists but fails this once — a forge outage, not a
// revocation — must not close public pages: the last known answer stands,
// as it does for any other transient error. Only a lost connection drops it.
func TestTransientForgeFailureKeepsPublicAnswer(t *testing.T) {
	bb := &fakeBB{err: errors.New("bitbucket: 503 service unavailable")}
	f, st := newForges(t, bb)
	connectedWorkspace(t, st, "acme")
	ctx := t.Context()
	repo := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "tok", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-48 * time.Hour)
	if err := st.SetRepoVisibility(ctx, repo.ID, store.VisibilityPublic, stale); err != nil {
		t.Fatal(err)
	}
	repo.Visibility, repo.VisibilityCheckedAt = store.VisibilityPublic, stale
	p := &Pipeline{Store: st, Forges: f, Log: f.Log}

	p.ReverifyVisibility(ctx, repo)
	if got, _ := st.RepoBySlug(ctx, "bitbucket", "acme/widgets"); got.Visibility != store.VisibilityPublic {
		t.Errorf("after a transient forge failure: visibility = %q, want the public answer kept", got.Visibility)
	}
	if !f.HasConnection(ctx, repo) {
		t.Error("HasConnection = false for a connected, unbroken grant")
	}
}

// rotatingApp is a GitHub App whose token mint lets an owner rotate the
// workspace's upload token mid-flight — the window between upkeep's read
// of the workspace and its flag write — and then answers with mintErr.
type rotatingApp struct {
	st      *storemem.Store
	mintErr error
}

func (a *rotatingApp) ForgeClient(ctx context.Context, _ int64) (forge.Forge, error) {
	ws, err := a.st.WorkspaceByPrefix(ctx, "github", "acme")
	if err != nil {
		return nil, err
	}
	ws.Token = "rotated-new" // as handleAPIWorkspaceRotate does
	if err := a.st.UpdateWorkspace(ctx, ws); err != nil {
		return nil, err
	}
	if a.mintErr != nil {
		return nil, a.mintErr
	}
	return forgefake.New(), nil
}
func (a *rotatingApp) InstallationAccount(context.Context, int64) (string, error) { return "acme", nil }
func (a *rotatingApp) InstallURL(context.Context) (string, error)                 { return "", nil }
func (a *rotatingApp) VerifyRunClaim(context.Context, int64, github.RunClaim) error {
	return nil
}

// Healing or marking the App connection's broken flag must not write back
// the rest of a workspace row read before the token mint: an upload token
// rotated meanwhile (because it leaked, say) would come back to life.
func TestAppFlagUpkeepKeepsARotationMadeMeanwhile(t *testing.T) {
	for name, tc := range map[string]struct {
		broken     bool
		mintErr    error
		wantBroken bool
	}{
		"heal":        {broken: true, wantBroken: false},
		"mark broken": {broken: false, mintErr: forge.ErrCredentialsRevoked, wantBroken: true},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			st := storemem.New()
			app := &rotatingApp{st: st, mintErr: tc.mintErr}
			f := NewForges(st, slog.New(slog.NewTextHandler(io.Discard, nil)), "https://cov.example.com", app, nil)
			ws := &store.Workspace{Forge: "github", Prefix: "acme", Token: "leaked-old", DefaultBranch: "main",
				GitHubInstallationID: 7, GitHubAppBroken: tc.broken}
			if err := st.CreateWorkspace(ctx, ws); err != nil {
				t.Fatal(err)
			}
			read, err := st.WorkspaceByPrefix(ctx, "github", "acme")
			if err != nil {
				t.Fatal(err)
			}
			f.Connected(ctx, read, "github")

			if _, err := st.WorkspaceByToken(ctx, "leaked-old"); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("the rotated-away token authenticates again (err = %v)", err)
			}
			got, err := st.WorkspaceByToken(ctx, "rotated-new")
			if err != nil {
				t.Fatalf("the new token no longer authenticates: %v", err)
			}
			if got.GitHubAppBroken != tc.wantBroken {
				t.Errorf("broken = %v, want %v", got.GitHubAppBroken, tc.wantBroken)
			}
		})
	}
}

// disconnectingStore performs an owner's disconnect (disconnectWorkspace's
// writes) the moment a refresh takes the grant lock: the read the caller
// holds is from before it.
type disconnectingStore struct {
	*storemem.Store
	f *Forges
}

func (s disconnectingStore) WithGrantLock(ctx context.Context, id int64, fn func(context.Context, store.GrantTx) error) error {
	if err := s.Store.SetWorkspaceGrant(ctx, id, "bitbucket", store.Grant{}); err != nil {
		return err
	}
	s.f.DropGrantToken("bitbucket", id)
	return s.Store.WithGrantLock(ctx, id, fn)
}

// A disconnect that lands while an upload is on its way to refreshing the
// grant must stay a disconnect: the upload gets no client, and the grant
// it read is not written back — not even as "broken", which the next
// upload would happily refresh from.
func TestDisconnectDuringRefreshStaysDisconnected(t *testing.T) {
	ctx := t.Context()
	bb := &fakeBB{}
	f, mem := newForges(t, bb)
	ws := connectedWorkspace(t, mem, "acme")
	f.Store = disconnectingStore{Store: mem, f: f}

	if fg := f.Connected(ctx, ws, "bitbucket"); fg != nil {
		t.Error("an upload racing a disconnect got a client")
	}
	f.Store = mem
	stored, err := mem.WorkspaceByPrefix(ctx, "bitbucket", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Grant != (store.Grant{}) {
		t.Errorf("stored grant = %+v, want it left disconnected", stored.Grant)
	}
	if fg := f.Connected(ctx, stored, "bitbucket"); fg != nil || bb.refreshes != 0 {
		t.Errorf("the next upload got a client (%v) or refreshed %d times; the disconnect was undone", fg != nil, bb.refreshes)
	}
}
