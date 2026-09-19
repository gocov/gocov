package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/auth"
	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	forgefake "github.com/gocov/gocov/internal/forge/fake"
	"github.com/gocov/gocov/internal/hosted"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// newWorkspaceFixture builds a private-mode server with sign-in enabled,
// the workspace acme (token ws-secret) and optionally the repo
// acme/widgets (token secret-token), then signs the owner in.
func newWorkspaceFixture(t *testing.T, withRepo bool) (*fixture, *http.Cookie) {
	t.Helper()
	return newWorkspaceFixtureAs(t, withRepo, memberIdentity())
}

// newMemberFixture is newWorkspaceFixture with the account seated as a
// plain member: the forge lists it in acme without the admin role.
func newMemberFixture(t *testing.T, withRepo bool) (*fixture, *http.Cookie) {
	t.Helper()
	return newWorkspaceFixtureAs(t, withRepo, plainMemberIdentity())
}

func newWorkspaceFixtureAs(t *testing.T, withRepo bool, id *auth.Identity) (*fixture, *http.Cookie) {
	t.Helper()
	st := storemem.New()
	ws := &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "ws-secret", DefaultBranch: "main"}
	if err := st.CreateWorkspace(t.Context(), ws); err != nil {
		t.Fatal(err)
	}
	var repo *store.Repo
	if withRepo {
		repo = &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "secret-token", DefaultBranch: "main"}
		if err := st.CreateRepo(t.Context(), repo); err != nil {
			t.Fatal(err)
		}
	}
	ff := forgefake.New()
	f := &fixture{
		srv: New(Config{
			Store:   st,
			Blobs:   blobmem.New(),
			Parsers: map[string]profile.Parser{"go": profile.GoParser{}},
			BaseURL: "https://gocov.example",
			Auths:   []auth.Provider{&fakeProvider{identity: id}},
		}),
		store: st,
		forge: ff,
		repo:  repo,
	}
	return f, signIn(t, f, "/")
}

// demote reseats the fixture's (only) user as a plain member of the
// workspace — what the next login sync would do after the forge dropped
// their admin role.
func demote(t *testing.T, f *fixture, prefix string) {
	t.Helper()
	ctx := t.Context()
	users, err := f.store.ListUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("users = %v, %v", users, err)
	}
	ws, err := f.store.WorkspaceByPrefix(ctx, users[0].Forge, prefix)
	if err != nil {
		t.Fatal(err)
	}
	memberships, err := f.store.ListMembershipsForUser(ctx, users[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range memberships {
		if memberships[i].WorkspaceID == ws.ID {
			memberships[i].Role = store.RoleMember
		}
	}
	if err := f.store.SetUserMemberships(ctx, users[0].ID, memberships); err != nil {
		t.Fatal(err)
	}
}

// The settings page route answers the access question and nothing else:
// a member gets the shell, a stranger never learns the workspace exists,
// and a signed-out visitor is sent to sign in. What the screen shows is
// the UI API's (TestAPIWorkspaceAccess).
func TestWorkspaceSettingsPageAccess(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true)

	for _, path := range []string{"/w/bitbucket/acme", "/workspace-settings/bitbucket/acme", "/workspace-setup/bitbucket/acme"} {
		rec := get(f, path, sess)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `id="root"`) {
			t.Errorf("member GET %s: status = %d, want the shell", path, rec.Code)
		}
		// The shell carries no data, so it never names the workspace's token.
		if strings.Contains(rec.Body.String(), "ws-secret") {
			t.Errorf("GET %s leaked the upload token:\n%s", path, rec.Body)
		}
	}

	// A workspace the user is no member of is a page like any other: the
	// shell carries nothing, and /api/ui answers the 404 that hides it.
	if err := f.store.CreateWorkspace(t.Context(),
		&store.Workspace{Forge: "bitbucket", Prefix: "beta", Token: "beta-tok", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if rec := get(f, "/api/ui/workspace-settings/bitbucket/beta", sess); rec.Code != http.StatusNotFound {
		t.Errorf("non-member workspace: status = %d, want 404", rec.Code)
	}
	// Without a session the auth middleware redirects to login.
	if rec := get(f, "/workspace-settings/bitbucket/acme"); rec.Code != http.StatusFound {
		t.Errorf("anonymous settings page: status = %d, want login redirect", rec.Code)
	}
}

func TestOwnerDemotedOnTheForgeLosesTheControls(t *testing.T) {
	// The role is whatever the last sign-in said; once it says member,
	// the owner-only endpoints close — no session or page state keeps them.
	f, sess := newWorkspaceFixture(t, false)
	save := func() *httptest.ResponseRecorder {
		return postJSON(t, f, "/api/ui/workspace-settings/save/bitbucket/acme",
			workspaceSettingsInput{DefaultBranch: "develop"}, sess)
	}
	wantStatus(t, save(), "owner save", http.StatusOK)
	demote(t, f, "acme")
	if rec := save(); rec.Code != http.StatusForbidden {
		t.Errorf("demoted save: status = %d, want 403", rec.Code)
	}
	if ws, _ := f.store.WorkspaceByPrefix(t.Context(), "bitbucket", "acme"); ws.DefaultBranch != "develop" {
		t.Errorf("demoted save went through: branch = %q", ws.DefaultBranch)
	}
}

func TestWorkspaceSettingsNeedAuthEnabled(t *testing.T) {
	// Open mode has no notion of members, so the workspace endpoints do not
	// exist (M2/D5: open mode stays byte-identical, no new surfaces).
	f := newFixture(t, nil)
	if err := f.store.CreateWorkspace(t.Context(),
		&store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "ws-secret", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/ui/workspace-settings/bitbucket/acme",
		"/api/ui/workspace-setup/bitbucket/acme",
		"/api/ui/workspace-setup-status/bitbucket/acme",
	} {
		if rec := get(f, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s in open mode: status = %d, want 404", path, rec.Code)
		}
	}
}

// Rotation is a live secret change, not just a stored value: the old
// token stops authenticating uploads the moment the new one is handed
// back (R3).
func TestWorkspaceRotateTokenKillsTheOldOne(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true)

	rec := doUpload(t, f, "ws-secret", map[string]string{
		"repo": "acme/widgets", "commit": "c1", "branch": "main"}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload with workspace token: status = %d, body = %s", rec.Code, rec.Body)
	}

	rot := postJSON(t, f, "/api/ui/workspace-settings/rotate-token/bitbucket/acme", nil, sess)
	wantStatus(t, rot, "rotate", http.StatusOK)
	token := decodeJSON[tokenRevealDTO](t, rot).Token
	if token == "" || token == "ws-secret" {
		t.Fatalf("rotated token = %q", token)
	}

	if rec := doUpload(t, f, "ws-secret", map[string]string{
		"repo": "acme/widgets", "commit": "c2", "branch": "main"}, testProfile); rec.Code != http.StatusUnauthorized {
		t.Errorf("old token after rotation: status = %d, want 401", rec.Code)
	}
	if rec := doUpload(t, f, token, map[string]string{
		"repo": "acme/widgets", "commit": "c2", "branch": "main"}, testProfile); rec.Code != http.StatusCreated {
		t.Errorf("new token: status = %d, body = %s", rec.Code, rec.Body)
	}

	// Reveal answers with the current token, never the pre-rotation one.
	reveal := postJSON(t, f, "/api/ui/workspace-settings/reveal-token/bitbucket/acme", nil, sess)
	if got := decodeJSON[tokenRevealDTO](t, reveal).Token; got != token {
		t.Errorf("revealed token = %q, want the rotated one", got)
	}
}

// Deleting a workspace takes its repos and their coverage with it, and a
// non-member cannot delete what they cannot see.
func TestWorkspaceDeleteCascades(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true)
	ctx := t.Context()

	if rec := doUpload(t, f, "ws-secret", map[string]string{
		"repo": "acme/widgets", "commit": "c1", "branch": "main"}, testProfile); rec.Code != http.StatusCreated {
		t.Fatalf("seed upload: status = %d", rec.Code)
	}
	if _, err := f.store.RepoBySlug(ctx, "bitbucket", "acme/widgets"); err != nil {
		t.Fatalf("repo not present before delete: %v", err)
	}

	wantStatus(t, postJSON(t, f, "/api/ui/workspace-settings/delete/bitbucket/acme", nil, sess), "delete", http.StatusNoContent)
	if _, err := f.store.WorkspaceByPrefix(ctx, "bitbucket", "acme"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("workspace survived delete: %v", err)
	}
	if _, err := f.store.RepoBySlug(ctx, "bitbucket", "acme/widgets"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("repo survived workspace delete (no cascade): %v", err)
	}

	f2, sess2 := newWorkspaceFixture(t, false)
	if err := f2.store.CreateWorkspace(ctx,
		&store.Workspace{Forge: "bitbucket", Prefix: "beta", Token: "beta-tok", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if rec := postJSON(t, f2, "/api/ui/workspace-settings/delete/bitbucket/beta", nil, sess2); rec.Code != http.StatusNotFound {
		t.Errorf("non-member delete: status = %d, want 404", rec.Code)
	}
	if _, err := f2.store.WorkspaceByPrefix(ctx, "bitbucket", "beta"); err != nil {
		t.Errorf("non-member delete removed the workspace: %v", err)
	}
}

// The CI snippet is written client-side from these inputs, so what the
// server must get right is the inputs themselves: which GitLab this
// instance faces (the catalog component lives on gitlab.com), and
// whether GOCOV_SERVER has to be spelled out at all.
func TestAPIWorkspaceSetupSnippetInputs(t *testing.T) {
	setup := func(t *testing.T, baseURL string, issuers []string) setupInfoDTO {
		t.Helper()
		st := storemem.New()
		if err := st.CreateWorkspace(t.Context(),
			&store.Workspace{Forge: "gitlab", Prefix: "grp/team", Token: "gl-secret", DefaultBranch: "main"}); err != nil {
			t.Fatal(err)
		}
		gl := &fakeProvider{name: "gitlab", identity: &auth.Identity{
			ForgeUUID: "9", DisplayName: "GL Dev",
			Workspaces: []string{"grp/team"}, OwnedWorkspaces: []string{"grp/team"},
		}}
		f := &fixture{
			srv: New(Config{
				Store:       st,
				Blobs:       blobmem.New(),
				Parsers:     map[string]profile.Parser{"go": profile.GoParser{}},
				BaseURL:     baseURL,
				Auths:       []auth.Provider{gl},
				OIDCIssuers: issuers,
			}),
			store: st,
		}
		sess := signInVia(t, f, "gitlab")
		return decodeJSON[setupInfoDTO](t, get(f, "/api/ui/workspace-setup/gitlab/grp/team", sess))
	}

	// A self-hosted instance facing gitlab.com: the component is offered
	// and the server URL has to be passed to it.
	got := setup(t, "https://gocov.example", nil)
	if !got.GitLabCatalog || got.ServerImplicit || got.BaseURL != "https://gocov.example" {
		t.Errorf("gitlab.com setup = %+v", got)
	}
	// A self-managed GitLab cannot include a component from gitlab.com's
	// catalog, so the app falls back to the raw download recipe.
	if got := setup(t, "https://gocov.example", []string{"https://gitlab.example"}); got.GitLabCatalog {
		t.Error("a self-managed GitLab must not be offered the gitlab.com component")
	}
	// On the hosted service the CLI already defaults to the server, so the
	// snippet drops GOCOV_SERVER entirely.
	if got := setup(t, hosted.DefaultServer, nil); !got.ServerImplicit {
		t.Errorf("hosted setup = %+v, want the server implicit", got)
	}
}

// A connection that exists but no longer works names the reconnect as
// what brings tokenless uploads back, rather than offering Connect.
func TestAPIWorkspaceSetupBrokenConnection(t *testing.T) {
	f, sess := newWorkspaceFixture(t, false)
	ws, err := f.store.WorkspaceByPrefix(t.Context(), "bitbucket", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetWorkspaceBitbucketGrant(t.Context(), ws.ID, "acme-ci", "rt", true); err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[setupInfoDTO](t, get(f, "/api/ui/workspace-setup/bitbucket/acme", sess))
	if got.Tokenless || !got.ConnectionBroken {
		t.Errorf("broken connection = tokenless %v, broken %v; want the token path back",
			got.Tokenless, got.ConnectionBroken)
	}
}

// TestGitLabSubgroupWorkspace covers D2 end to end at the UI layer: a
// workspace registered at subgroup depth ("grp/sub") admits its member,
// serves its pages behind a %2F-encoded prefix, and scopes visibility to
// projects below the subgroup.
func TestGitLabSubgroupWorkspace(t *testing.T) {
	ctx := t.Context()
	st := storemem.New()
	ws := &store.Workspace{Forge: "gitlab", Prefix: "grp/sub", Token: "ws-secret", DefaultBranch: "main"}
	if err := st.CreateWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	repo := &store.Repo{Forge: "gitlab", Slug: "grp/sub/proj", Token: "repo-token", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	other := &store.Repo{Forge: "gitlab", Slug: "grp/elsewhere", Token: "other-token", DefaultBranch: "main"}
	if err := st.CreateRepo(ctx, other); err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		srv: New(Config{
			Store:   st,
			Blobs:   blobmem.New(),
			Parsers: map[string]profile.Parser{"go": profile.GoParser{}},
			BaseURL: "https://gocov.example",
			Auths: []auth.Provider{&fakeProvider{name: "gitlab", identity: &auth.Identity{
				ForgeUUID:   "12345",
				DisplayName: "Jane Dev",
				Email:       "jane@example.com",
				// The forge reports the subgroup's full path, not its root.
				Workspaces: []string{"grp/sub", "janedev"},
			}}},
		}),
		store: st,
	}
	sess := signInVia(t, f, "gitlab")

	// A nested prefix is a slash everywhere: the pages and their endpoints
	// take it as the trailing wildcard, never as one %2F segment.
	for _, path := range []string{
		"/w/gitlab/grp/sub", "/workspace-settings/gitlab/grp/sub", "/workspace-setup/gitlab/grp/sub",
		"/api/ui/workspace-settings/gitlab/grp/sub", "/api/ui/workspace-setup/gitlab/grp/sub",
	} {
		if rec := get(f, path, sess); rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200", path, rec.Code)
		}
	}
	// The parent group is not the subgroup: membership is of grp/sub alone.
	if rec := get(f, "/api/ui/workspace-settings/gitlab/grp", sess); rec.Code != http.StatusNotFound {
		t.Errorf("parent group settings: status = %d, want 404", rec.Code)
	}

	// Membership at subgroup depth scopes repo visibility: the subgroup's
	// project is visible, the sibling project outside it is not.
	if rec := get(f, "/repos/gitlab/grp/sub/proj", sess); rec.Code != http.StatusOK {
		t.Errorf("member repo page: status = %d, want 200", rec.Code)
	}
	if rec := get(f, "/repos/gitlab/grp/elsewhere", sess); rec.Code != http.StatusNotFound {
		t.Errorf("non-member repo page: status = %d, want 404", rec.Code)
	}
	// The subgroup's own repo is what its setup screen counts.
	if got := decodeJSON[setupInfoDTO](t, get(f, "/api/ui/workspace-setup/gitlab/grp/sub", sess)); got.Status.RepoCount != 1 {
		t.Errorf("subgroup setup repo count = %d, want its one project", got.Status.RepoCount)
	}
}

func TestAPIWorkspaceSettings(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true)

	got := decodeJSON[workspaceSettingsDTO](t, get(f, "/api/ui/workspace-settings/bitbucket/acme", sess))
	if got.Workspace.Prefix != "acme" || got.Workspace.ForgeLabel != "Bitbucket" {
		t.Errorf("workspace = %+v", got.Workspace)
	}
	if !got.Owner || got.RepoCount != 1 {
		t.Errorf("owner = %v, repo count = %d, want true/1", got.Owner, got.RepoCount)
	}
	if got.TokenMasked == nil || strings.Contains(*got.TokenMasked, "ws-secret") {
		t.Errorf("token_masked = %v, want the masked form", got.TokenMasked)
	}
	// This deployment has no connect mechanism wired, so the card says so.
	if got.Reporting.Available || got.Reporting.State != "off" || got.Reporting.ConnectURL != "" {
		t.Errorf("reporting = %+v", got.Reporting)
	}
	// A self-hosted base URL is what CI needs; the hosted one is implicit.
	if got.ServerURL == nil || *got.ServerURL != "https://gocov.example" {
		t.Errorf("server url = %v", got.ServerURL)
	}

	// Saving comes back as the saved settings.
	saved := postJSON(t, f, "/api/ui/workspace-settings/save/bitbucket/acme", workspaceSettingsInput{
		DefaultBranch:       "develop",
		ReportRetentionDays: 90,
		Gate:                gateDTO{MinCoverage: new(float64(75))},
	}, sess)
	wantStatus(t, saved, "save", http.StatusOK)
	out := decodeJSON[workspaceSettingsDTO](t, saved)
	if out.Workspace.DefaultBranch != "develop" || out.Workspace.ReportRetentionDays != 90 {
		t.Errorf("saved workspace = %+v", out.Workspace)
	}
	if out.Workspace.Gate.MinCoverage == nil || *out.Workspace.Gate.MinCoverage != 75 {
		t.Errorf("saved gate = %+v", out.Workspace.Gate)
	}
	ws, err := f.store.WorkspaceByPrefix(t.Context(), "bitbucket", "acme")
	if err != nil || ws.DefaultBranch != "develop" {
		t.Fatalf("stored workspace = %+v, %v", ws, err)
	}

	// Rotation hands back the new token, and it is the stored one.
	rec := postJSON(t, f, "/api/ui/workspace-settings/rotate-token/bitbucket/acme", nil, sess)
	wantStatus(t, rec, "rotate", http.StatusOK)
	rotated := decodeJSON[tokenRevealDTO](t, rec).Token
	if rotated == "" || rotated == "ws-secret" {
		t.Errorf("rotated token = %q", rotated)
	}
	if ws, _ = f.store.WorkspaceByPrefix(t.Context(), "bitbucket", "acme"); ws.Token != rotated {
		t.Errorf("stored token = %q, want the rotated one", ws.Token)
	}

	// Delete answers with no content and cascades.
	del := postJSON(t, f, "/api/ui/workspace-settings/delete/bitbucket/acme", nil, sess)
	wantStatus(t, del, "delete", http.StatusNoContent)
	if _, err := f.store.WorkspaceByPrefix(t.Context(), "bitbucket", "acme"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("workspace survived the delete: %v", err)
	}
}

func TestAPIWorkspaceSettingsValidation(t *testing.T) {
	f, sess := newWorkspaceFixture(t, false)
	for _, tc := range []struct {
		name string
		in   workspaceSettingsInput
	}{
		{"empty branch", workspaceSettingsInput{DefaultBranch: "  "}},
		{"gate out of range", workspaceSettingsInput{DefaultBranch: "main", Gate: gateDTO{MinCoverage: new(float64(150))}}},
		{"unknown retention", workspaceSettingsInput{DefaultBranch: "main", ReportRetentionDays: 7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSON(t, f, "/api/ui/workspace-settings/save/bitbucket/acme", tc.in, sess)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %s)", rec.Code, rec.Body)
			}
			if decodeJSON[struct {
				Error string `json:"error"`
			}](t, rec).Error == "" {
				t.Error("no message to show the user")
			}
		})
	}
	// A field the app does not know about is a mistake, not a default.
	rec := postJSON(t, f, "/api/ui/workspace-settings/save/bitbucket/acme",
		map[string]any{"default_branch": "main", "nonsense": 1}, sess)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: status = %d, want 400", rec.Code)
	}
	if ws, _ := f.store.WorkspaceByPrefix(t.Context(), "bitbucket", "acme"); ws.DefaultBranch != "main" {
		t.Errorf("a refused save changed the workspace: %+v", ws)
	}
}

// The API's access rules are the pages': signed out is a 401, a
// non-member never learns the workspace exists, and a member reads but
// does not write.
func TestAPIWorkspaceAccess(t *testing.T) {
	f, sess := newWorkspaceFixture(t, false)
	if rec := get(f, "/api/ui/workspace-settings/bitbucket/acme"); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed-out GET: status = %d, want 401", rec.Code)
	}
	if err := f.store.CreateWorkspace(t.Context(),
		&store.Workspace{Forge: "bitbucket", Prefix: "beta", Token: "bt", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if rec := get(f, "/api/ui/workspace-settings/bitbucket/beta", sess); rec.Code != http.StatusNotFound {
		t.Errorf("non-member GET: status = %d, want 404", rec.Code)
	}

	member, msess := newMemberFixture(t, false)
	if rec := get(member, "/api/ui/workspace-settings/bitbucket/acme", msess); rec.Code != http.StatusOK {
		t.Fatalf("member GET: status = %d", rec.Code)
	}
	if got := decodeJSON[workspaceSettingsDTO](t, get(member, "/api/ui/workspace-settings/bitbucket/acme", msess)); got.Owner || got.TokenMasked != nil {
		t.Errorf("member settings = %+v, want no ownership and no token", got)
	}
	for _, path := range []string{
		"/api/ui/workspace-settings/save/bitbucket/acme",
		"/api/ui/workspace-settings/rotate-token/bitbucket/acme",
		"/api/ui/workspace-settings/reveal-token/bitbucket/acme",
		"/api/ui/workspace-settings/disconnect/bitbucket/acme",
		"/api/ui/workspace-settings/delete/bitbucket/acme",
	} {
		rec := postJSON(t, member, path, workspaceSettingsInput{DefaultBranch: "develop"}, msess)
		if rec.Code != http.StatusForbidden {
			t.Errorf("member POST %s: status = %d, want 403", path, rec.Code)
		}
	}
	ws, err := member.store.WorkspaceByPrefix(t.Context(), "bitbucket", "acme")
	if err != nil || ws.Token != "ws-secret" || ws.DefaultBranch != "main" {
		t.Errorf("a member's refused POSTs changed the workspace: %+v, %v", ws, err)
	}
}
