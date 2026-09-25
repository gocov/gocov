package memory

import (
	"context"
	"errors"
	"reflect"
	"testing"

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
