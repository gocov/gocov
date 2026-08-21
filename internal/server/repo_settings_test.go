package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/store"
)

func TestRepoSettingsAccess(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true) // workspace acme + repo acme/widgets, member signed in

	rec := get(f, "/repo-settings/acme/widgets", sess)
	if rec.Code != http.StatusOK {
		t.Fatalf("member settings page: status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "secret-token") {
		t.Error("settings page should make the upload token available to members (Reveal)")
	}
	for _, want := range []string{"Coverage gates", "Base branch", "Remove repository"} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page missing %q", want)
		}
	}

	// A repo whose workspace the user is no member of 404s, even though it exists.
	if err := f.store.CreateWorkspace(context.Background(),
		&store.Workspace{Forge: "bitbucket", Prefix: "beta", Token: "bt", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.CreateRepo(context.Background(),
		&store.Repo{Forge: "bitbucket", Slug: "beta/thing", Token: "x", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if rec := get(f, "/repo-settings/beta/thing", sess); rec.Code != http.StatusNotFound {
		t.Errorf("non-member settings page: status = %d, want 404", rec.Code)
	}
	// Anonymous is redirected to login by the auth middleware.
	if rec := get(f, "/repo-settings/acme/widgets"); rec.Code != http.StatusFound {
		t.Errorf("anonymous settings page: status = %d, want login redirect", rec.Code)
	}
}

func TestRepoSettingsSaveRotateDelete(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true)
	ctx := context.Background()

	// Save base branch + a min-coverage gate.
	rec := postForm(f, "/repo-settings/save/acme/widgets", url.Values{
		"default_branch": {"develop"}, "min_coverage": {"85"},
	}, sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: status = %d, want 303", rec.Code)
	}
	repo, err := f.store.RepoBySlug(ctx, "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	if repo.DefaultBranch != "develop" {
		t.Errorf("base branch not saved: %q", repo.DefaultBranch)
	}
	if repo.Gate.MinCoverage == nil || *repo.Gate.MinCoverage != 85 {
		t.Errorf("gate not saved: %+v", repo.Gate)
	}

	// A bad gate value is rejected.
	if rec := postForm(f, "/repo-settings/save/acme/widgets", url.Values{
		"default_branch": {"main"}, "min_coverage": {"250"},
	}, sess); rec.Code != http.StatusBadRequest {
		t.Errorf("bad gate: status = %d, want 400", rec.Code)
	}

	// Rotate the token: a new one is issued and shown once.
	rec = postForm(f, "/repo-settings/rotate-token/acme/widgets", url.Values{}, sess)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate: status = %d", rec.Code)
	}
	repo, _ = f.store.RepoBySlug(ctx, "acme/widgets")
	if repo.Token == "secret-token" {
		t.Error("token was not rotated")
	}
	if !strings.Contains(rec.Body.String(), repo.Token) {
		t.Error("rotated token not shown once in the response")
	}

	// Delete removes the repo and redirects to the workspace.
	rec = postForm(f, "/repo-settings/delete/acme/widgets", url.Values{}, sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: status = %d, want 303", rec.Code)
	}
	if _, err := f.store.RepoBySlug(ctx, "acme/widgets"); err == nil {
		t.Error("repo still present after delete")
	}
}
