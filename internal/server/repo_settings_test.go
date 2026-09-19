package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/store"
)

// The settings page route answers the access question and serves the
// shell; what the screen shows is the UI API's (TestAPIRepoSettings).
func TestRepoSettingsPageAccess(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true) // workspace acme + repo acme/widgets, owner signed in

	rec := get(f, "/repo-settings/bitbucket/acme/widgets", sess)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Fatalf("member settings page: status = %d, want the shell", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret-token") {
		t.Errorf("the settings shell leaked the upload token:\n%s", rec.Body)
	}
	// Anonymous is redirected to login by the auth middleware.
	if rec := get(f, "/repo-settings/bitbucket/acme/widgets"); rec.Code != http.StatusFound {
		t.Errorf("anonymous settings page: status = %d, want login redirect", rec.Code)
	}
}

// The Ignored files card saves one pattern per line: blank lines and
// comments drop out, a CRLF textarea normalises, and clearing the field
// clears the patterns.
func TestAPIRepoSettingsIgnorePaths(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true)
	ctx := t.Context()
	save := func(patterns string) *httptest.ResponseRecorder {
		t.Helper()
		return postJSON(t, f, "/api/ui/repo-settings/save/bitbucket/acme/widgets",
			repoSettingsInput{DefaultBranch: "main", IgnorePaths: patterns}, sess)
	}

	rec := save("cmd/preview/**\r\n\r\n# generated\r\n*_mock.go\r\n")
	wantStatus(t, rec, "save", http.StatusOK)
	if got := decodeJSON[repoSettingsDTO](t, rec).Repo.IgnorePaths; got != "cmd/preview/**\n*_mock.go" {
		t.Errorf("patterns read back as %q", got)
	}
	repo, err := f.store.RepoBySlug(ctx, "bitbucket", "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cmd/preview/**", "*_mock.go"}; !slices.Equal(repo.IgnorePaths, want) {
		t.Errorf("ignore paths = %q, want %q", repo.IgnorePaths, want)
	}

	// An uncompilable pattern is refused and nothing changes.
	if rec := save("src/["); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "ignore pattern") {
		t.Errorf("bad pattern: status = %d, body = %s", rec.Code, rec.Body)
	}
	if repo, _ = f.store.RepoBySlug(ctx, "bitbucket", "acme/widgets"); len(repo.IgnorePaths) != 2 {
		t.Errorf("a refused save changed the patterns: %q", repo.IgnorePaths)
	}

	wantStatus(t, save(""), "clear", http.StatusOK)
	if repo, _ = f.store.RepoBySlug(ctx, "bitbucket", "acme/widgets"); repo.IgnorePaths != nil {
		t.Errorf("patterns not cleared: %q", repo.IgnorePaths)
	}
}

func TestAPIRepoSettings(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true)

	got := decodeJSON[repoSettingsDTO](t, get(f, "/api/ui/repo-settings/bitbucket/acme/widgets", sess))
	if got.Repo.Slug != "acme/widgets" || got.Workspace.Prefix != "acme" {
		t.Errorf("repo = %+v, workspace = %+v", got.Repo, got.Workspace)
	}
	if !got.Owner || got.TokenMasked == nil || strings.Contains(*got.TokenMasked, "secret-token") {
		t.Errorf("owner = %v, token_masked = %v", got.Owner, got.TokenMasked)
	}
	// The instance does not allow public reports, so the switch is not a
	// choice this repo has.
	if got.ShowPublicReports {
		t.Error("public reports offered on an instance that does not allow them")
	}
	if got.Repo.BadgeURL != "/badge/bitbucket/acme/widgets.svg" || got.Repo.BadgeMarkdown == "" {
		t.Errorf("badge = %q / %q", got.Repo.BadgeURL, got.Repo.BadgeMarkdown)
	}

	saved := postJSON(t, f, "/api/ui/repo-settings/save/bitbucket/acme/widgets", repoSettingsInput{
		DefaultBranch: "develop",
		Gate:          gateDTO{MinCoverage: new(float64(80)), MaxCoverageDrop: new(float64(1))},
		IgnorePaths:   "vendor/**\n*_test.go",
	}, sess)
	wantStatus(t, saved, "save", http.StatusOK)
	out := decodeJSON[repoSettingsDTO](t, saved)
	if out.Repo.DefaultBranch != "develop" || out.Repo.IgnorePaths != "vendor/**\n*_test.go" {
		t.Errorf("saved repo = %+v", out.Repo)
	}
	repo, err := f.store.RepoBySlug(t.Context(), "bitbucket", "acme/widgets")
	if err != nil || repo.Gate.MinCoverage == nil || *repo.Gate.MinCoverage != 80 ||
		!slices.Equal(repo.IgnorePaths, []string{"vendor/**", "*_test.go"}) {
		t.Fatalf("stored repo = %+v, %v", repo, err)
	}

	rec := postJSON(t, f, "/api/ui/repo-settings/rotate-token/bitbucket/acme/widgets", nil, sess)
	wantStatus(t, rec, "rotate", http.StatusOK)
	rotated := decodeJSON[tokenRevealDTO](t, rec).Token
	if rotated == "" || rotated == "secret-token" {
		t.Errorf("rotated token = %q", rotated)
	}
	if repo, _ = f.store.RepoBySlug(t.Context(), "bitbucket", "acme/widgets"); repo.Token != rotated {
		t.Errorf("stored token = %q, want the rotated one", repo.Token)
	}

	del := postJSON(t, f, "/api/ui/repo-settings/delete/bitbucket/acme/widgets", nil, sess)
	wantStatus(t, del, "delete", http.StatusNoContent)
	if _, err := f.store.RepoBySlug(t.Context(), "bitbucket", "acme/widgets"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("repo survived the delete: %v", err)
	}
}

func TestAPIRepoSettingsValidation(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true)
	for _, tc := range []struct {
		name string
		in   repoSettingsInput
	}{
		{"empty branch", repoSettingsInput{DefaultBranch: ""}},
		{"gate out of range", repoSettingsInput{DefaultBranch: "main", Gate: gateDTO{MinDiffCoverage: new(float64(-1))}}},
		{"bad ignore pattern", repoSettingsInput{DefaultBranch: "main", IgnorePaths: "src/[unclosed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSON(t, f, "/api/ui/repo-settings/save/bitbucket/acme/widgets", tc.in, sess)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %s)", rec.Code, rec.Body)
			}
		})
	}
	if repo, _ := f.store.RepoBySlug(t.Context(), "bitbucket", "acme/widgets"); repo.DefaultBranch != "main" {
		t.Errorf("a refused save changed the repo: %+v", repo)
	}
}

func TestAPIRepoSettingsAccess(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true)
	if rec := get(f, "/api/ui/repo-settings/bitbucket/acme/widgets"); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed-out GET: status = %d, want 401", rec.Code)
	}
	// A repo outside the viewer's workspaces must not even exist for them.
	ctx := t.Context()
	if err := f.store.CreateWorkspace(ctx, &store.Workspace{Forge: "bitbucket", Prefix: "beta", Token: "bt", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.CreateRepo(ctx, &store.Repo{Forge: "bitbucket", Slug: "beta/thing", Token: "x", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if rec := get(f, "/api/ui/repo-settings/bitbucket/beta/thing", sess); rec.Code != http.StatusNotFound {
		t.Errorf("non-member GET: status = %d, want 404", rec.Code)
	}

	member, msess := newMemberFixture(t, true)
	got := decodeJSON[repoSettingsDTO](t, get(member, "/api/ui/repo-settings/bitbucket/acme/widgets", msess))
	if got.Owner || got.TokenMasked != nil {
		t.Errorf("member settings = %+v, want no ownership and no token", got)
	}
	for _, path := range []string{
		"/api/ui/repo-settings/save/bitbucket/acme/widgets",
		"/api/ui/repo-settings/rotate-token/bitbucket/acme/widgets",
		"/api/ui/repo-settings/reveal-token/bitbucket/acme/widgets",
		"/api/ui/repo-settings/delete/bitbucket/acme/widgets",
	} {
		rec := postJSON(t, member, path, repoSettingsInput{DefaultBranch: "develop"}, msess)
		if rec.Code != http.StatusForbidden {
			t.Errorf("member POST %s: status = %d, want 403", path, rec.Code)
		}
	}
	repo, err := member.store.RepoBySlug(t.Context(), "bitbucket", "acme/widgets")
	if err != nil || repo.Token != "secret-token" || repo.DefaultBranch != "main" {
		t.Errorf("a member's refused POSTs changed the repo: %+v, %v", repo, err)
	}
}
