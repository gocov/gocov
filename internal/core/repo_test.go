package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/forge"
	forgefake "github.com/gocov/gocov/internal/forge/fake"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

func TestValidRepoName(t *testing.T) {
	tests := []struct {
		forge, name string
		want        bool
	}{
		{"bitbucket", "widgets", true},
		{"bitbucket", "sub/widgets", false},
		{"github", "sub/widgets", false},
		{"gitlab", "widgets", true},
		{"gitlab", "sub/widgets", true},
		{"gitlab", "sub/team/widgets", true},
		{"gitlab", "sub//widgets", false},
		{"gitlab", "sub/../widgets", false},
		{"gitlab", "", false},
	}
	for _, tt := range tests {
		if got := ValidRepoName(tt.forge, tt.name); got != tt.want {
			t.Errorf("ValidRepoName(%q, %q) = %v, want %v", tt.forge, tt.name, got, tt.want)
		}
	}
}

func TestRefreshVisibilityCachesForgeAnswer(t *testing.T) {
	p, st, repo := newPipeline(t, store.Gate{})
	ctx := t.Context()

	fg := forgefake.New()
	fg.Visibility = forge.VisibilityPublic
	p.RefreshVisibility(ctx, fg, repo)
	if repo.Visibility != store.VisibilityPublic {
		t.Errorf("repo.Visibility = %q, want public", repo.Visibility)
	}
	stored, err := st.RepoBySlug(ctx, repo.Forge, repo.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Visibility != store.VisibilityPublic {
		t.Errorf("stored visibility = %q, want public", stored.Visibility)
	}
	if stored.VisibilityCheckedAt.IsZero() {
		t.Error("a definitive answer did not stamp VisibilityCheckedAt")
	}

	// An unchanged answer still refreshes the stamp — the freshness
	// windows count from the last answer, not the last change.
	old := time.Unix(1, 0)
	repo.VisibilityCheckedAt = old
	p.RefreshVisibility(ctx, fg, repo)
	if !repo.VisibilityCheckedAt.After(old) {
		t.Error("an unchanged answer did not refresh the checked-at stamp")
	}

	// Flipped private on the forge: the cache follows.
	fg.Visibility = forge.VisibilityPrivate
	p.RefreshVisibility(ctx, fg, repo)
	if stored, _ := st.RepoBySlug(ctx, repo.Forge, repo.Slug); stored.Visibility != store.VisibilityPrivate {
		t.Errorf("stored visibility after flip = %q, want private", stored.Visibility)
	}

	// A forge that cannot answer keeps the last known state — and the
	// old stamp; so does having no forge at all, and so does an answer
	// outside the public/private contract — it is rejected, never cached
	// verbatim.
	repo.VisibilityCheckedAt = old
	fg.VisibilityErr = errors.New("forge down")
	p.RefreshVisibility(ctx, fg, repo)
	p.RefreshVisibility(ctx, nil, repo)
	fg.VisibilityErr = nil
	fg.Visibility = "internal"
	p.RefreshVisibility(ctx, fg, repo)
	if stored, _ := st.RepoBySlug(ctx, repo.Forge, repo.Slug); stored.Visibility != store.VisibilityPrivate {
		t.Errorf("stored visibility after failures = %q, want private", stored.Visibility)
	}
	if !repo.VisibilityCheckedAt.Equal(old) {
		t.Error("a failed refresh advanced the checked-at stamp")
	}
}

func TestRefreshVisibilityFailsClosedWhenRepoIsGone(t *testing.T) {
	p, st, repo := newPipeline(t, store.Gate{})
	ctx := t.Context()

	fg := forgefake.New()
	fg.Visibility = forge.VisibilityPublic
	p.RefreshVisibility(ctx, fg, repo)

	// The forge positively saying the repo is gone (deleted, or hidden
	// from a connection that lost access) is a definitive answer, not a
	// transient failure: certainly not public any more.
	fg.VisibilityErr = fmt.Errorf("wrapped: %w", forge.ErrRepoNotFound)
	p.RefreshVisibility(ctx, fg, repo)
	if repo.Visibility != store.VisibilityPrivate {
		t.Errorf("repo.Visibility after not-found = %q, want private", repo.Visibility)
	}
	if stored, _ := st.RepoBySlug(ctx, repo.Forge, repo.Slug); stored.Visibility != store.VisibilityPrivate {
		t.Errorf("stored visibility after not-found = %q, want private", stored.Visibility)
	}
}

// TestReverifyVisibilityIfStale covers the serving-path half of the
// staleness mechanism: a stale cached answer is re-checked through the
// repo's forge connection in the background, a fresh one is not, and
// attempts are rate-limited per repo.
func TestReverifyVisibilityIfStale(t *testing.T) {
	ctx := t.Context()
	ff := forgefake.New()
	ff.Visibility = forge.VisibilityPrivate
	forges, st := newForges(t, &fakeBB{client: ff})
	connectedWorkspace(t, st, "acme")
	repo := &store.Repo{
		Forge: "bitbucket", Slug: "acme/widgets", Token: "tok-vis",
		DefaultBranch: "main", Visibility: store.VisibilityPublic,
	}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	p := &Pipeline{Store: st, Log: forges.Log, Forges: forges}

	// A zero stamp is maximally stale: the re-check starts and, the forge
	// now answering private, closes the repo.
	if !p.ReverifyVisibilityIfStale(repo) {
		t.Fatal("stale answer did not start a re-check")
	}
	waitForVisibility(t, st, repo.Slug, store.VisibilityPrivate)

	// A second attempt within the rate-limit gap is refused, however
	// stale the caller's struct still looks.
	if p.ReverifyVisibilityIfStale(repo) {
		t.Error("second attempt within the gap was not rate-limited")
	}

	// A fresh stamp never starts a check, and neither does a pipeline
	// without forge connections.
	fresh := *repo
	fresh.ID = repo.ID + 1000 // dodge the rate limiter: freshness must refuse first
	fresh.VisibilityCheckedAt = time.Now()
	if p.ReverifyVisibilityIfStale(&fresh) {
		t.Error("fresh answer started a re-check")
	}
	if (&Pipeline{Store: st, Log: forges.Log}).ReverifyVisibilityIfStale(repo) {
		t.Error("pipeline without Forges started a re-check")
	}
}

// TestSetRepoVisibilityIgnoresStaleAnswers pins the memory double to the
// Store contract's answer ordering: a write whose ask predates the stored
// stamp lost the race and must be skipped, or an in-flight refresh could
// overwrite a fresher webhook-delivered flip.
func TestSetRepoVisibilityIgnoresStaleAnswers(t *testing.T) {
	_, st, repo := newPipeline(t, store.Gate{})
	ctx := t.Context()

	now := time.Now()
	if err := st.SetRepoVisibility(ctx, repo.ID, store.VisibilityPrivate, now); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepoVisibility(ctx, repo.ID, store.VisibilityPublic, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if stored, _ := st.RepoBySlug(ctx, repo.Forge, repo.Slug); stored.Visibility != store.VisibilityPrivate {
		t.Errorf("a stale answer overwrote a fresher one: %q", stored.Visibility)
	}
	if err := st.SetRepoVisibility(ctx, repo.ID, store.VisibilityPublic, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if stored, _ := st.RepoBySlug(ctx, repo.Forge, repo.Slug); stored.Visibility != store.VisibilityPublic {
		t.Errorf("a fresher answer was refused: %q", stored.Visibility)
	}
}

// TestVisibilityRefreshInFlightGuard covers the per-repo claim that keeps
// a commit's concurrently uploading parts from all asking the forge the
// same visibility question.
func TestVisibilityRefreshInFlightGuard(t *testing.T) {
	p := &Pipeline{}
	if !p.beginVisibilityRefresh(1) {
		t.Fatal("first claim refused")
	}
	if p.beginVisibilityRefresh(1) {
		t.Error("concurrent claim for the same repo allowed")
	}
	if !p.beginVisibilityRefresh(2) {
		t.Error("an unrelated repo was blocked")
	}
	p.endVisibilityRefresh(1)
	if !p.beginVisibilityRefresh(1) {
		t.Error("claim after release refused")
	}
}

// waitForVisibility polls the store until the repo's cached visibility
// matches — the background re-check runs on its own goroutine.
func waitForVisibility(t *testing.T, st *storemem.Store, slug, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		stored, err := st.RepoBySlug(t.Context(), "bitbucket", slug)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Visibility == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("visibility = %q, want %q (background re-check never landed)", stored.Visibility, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRegisterRepo(t *testing.T) {
	gate := store.Gate{MinCoverage: new(80.0)}
	for _, tc := range []struct {
		name       string
		connected  bool   // workspace has a working one-click connection
		wsBranch   string // workspace default branch
		forgeSetup func(*forgefake.Forge)
		wantBranch string
		wantErr    error
	}{
		{
			name: "forge answers the default branch", connected: true, wsBranch: "main",
			forgeSetup: func(f *forgefake.Forge) { f.DefaultBranch = "trunk" },
			wantBranch: "trunk",
		},
		{
			name: "forge without the endpoint falls back to the workspace", connected: true, wsBranch: "develop",
			wantBranch: "develop",
		},
		{
			name: "transient forge error falls back to the workspace", connected: true, wsBranch: "develop",
			forgeSetup: func(f *forgefake.Forge) { f.DefaultBranchErr = errors.New("timeout") },
			wantBranch: "develop",
		},
		{
			name:       "no connection and no workspace branch falls back to main",
			wantBranch: "main",
		},
		{
			// A leaked workspace token must not fill the dashboard with
			// repos the forge says do not exist.
			name: "forge denying the repo aborts the registration", connected: true,
			forgeSetup: func(f *forgefake.Forge) { f.DefaultBranchErr = forge.ErrRepoNotFound },
			wantErr:    forge.ErrRepoNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			client := forgefake.New()
			if tc.forgeSetup != nil {
				tc.forgeSetup(client)
			}
			forges, st := newForges(t, &fakeBB{client: client})
			p := &Pipeline{Store: st, Log: forges.Log, Forges: forges}

			ws := &store.Workspace{Forge: "bitbucket", Prefix: "acme", DefaultBranch: tc.wsBranch, Gate: gate}
			if tc.connected {
				ws = connectedWorkspace(t, st, "acme")
				ws.DefaultBranch, ws.Gate = tc.wsBranch, gate
			}

			repo, err := p.RegisterRepo(ctx, ws, "acme/widgets")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if _, err := st.RepoBySlug(ctx, "bitbucket", "acme/widgets"); !errors.Is(err, store.ErrNotFound) {
					t.Errorf("aborted registration left a repo behind (lookup err = %v)", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if repo.DefaultBranch != tc.wantBranch {
				t.Errorf("default branch = %q, want %q", repo.DefaultBranch, tc.wantBranch)
			}
			if repo.Forge != "bitbucket" || repo.Gate.MinCoverage == nil || *repo.Gate.MinCoverage != 80 {
				t.Errorf("repo = %+v, want the workspace's forge and gate", repo)
			}
			if len(repo.Token) != 48 {
				t.Errorf("token %q is not 24 hex-encoded bytes", repo.Token)
			}
			stored, err := st.RepoBySlug(ctx, "bitbucket", "acme/widgets")
			if err != nil {
				t.Fatal(err)
			}
			if stored.ID != repo.ID {
				t.Errorf("stored repo id = %d, want %d", stored.ID, repo.ID)
			}
		})
	}
}

func TestRegisterRepoLosingTheRaceReturnsTheWinner(t *testing.T) {
	ctx := t.Context()
	p, st, existing := newPipeline(t, store.Gate{})
	ws := &store.Workspace{Forge: existing.Forge, Prefix: "acme"}

	repo, err := p.RegisterRepo(ctx, ws, existing.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if repo.ID != existing.ID || repo.Token != existing.Token {
		t.Errorf("got repo %+v, want the concurrently registered %+v", repo, existing)
	}
	repos, err := st.ListRepos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Errorf("repos = %d, want 1", len(repos))
	}
}

func TestMarkRepoPrivate(t *testing.T) {
	ctx := t.Context()
	p, st, repo := newPipeline(t, store.Gate{})
	repo.Visibility = store.VisibilityPublic

	p.MarkRepoPrivate(ctx, repo)
	if repo.Visibility != store.VisibilityPrivate {
		t.Errorf("repo.Visibility = %q, want private", repo.Visibility)
	}
	stored, err := st.RepoBySlug(ctx, repo.Forge, repo.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Visibility != store.VisibilityPrivate || stored.VisibilityCheckedAt.IsZero() {
		t.Errorf("stored = %q at %v, want private with a stamp", stored.Visibility, stored.VisibilityCheckedAt)
	}

	// A store that cannot persist the flip leaves the in-memory repo as it
	// was, so the request does not act on a state nobody recorded.
	gone := &store.Repo{ID: repo.ID + 100, Slug: "acme/gone", Visibility: store.VisibilityPublic}
	p.MarkRepoPrivate(ctx, gone)
	if gone.Visibility != store.VisibilityPublic {
		t.Errorf("unpersisted flip changed the repo to %q", gone.Visibility)
	}
}

func TestNewToken(t *testing.T) {
	a, b := NewToken(), NewToken()
	if len(a) != 48 || strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("token %q is not 48 lowercase hex characters", a)
	}
	if a == b {
		t.Error("two tokens collided")
	}
}
