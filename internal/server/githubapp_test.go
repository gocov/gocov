package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/auth"
	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/forge"
	forgefake "github.com/gocov/gocov/internal/forge/fake"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// fakeGitHubApp is a canned server.GitHubApp: installations resolve via
// the accounts map, and ForgeClient hands out appForge (or forgeErr).
type fakeGitHubApp struct {
	appForge   forge.Forge
	forgeErr   error
	accounts   map[int64]string
	installURL string
	forgeCalls []int64
}

func (f *fakeGitHubApp) ForgeClient(_ context.Context, id int64) (forge.Forge, error) {
	f.forgeCalls = append(f.forgeCalls, id)
	if f.forgeErr != nil {
		return nil, f.forgeErr
	}
	return f.appForge, nil
}

func (f *fakeGitHubApp) InstallationAccount(_ context.Context, id int64) (string, error) {
	if login, ok := f.accounts[id]; ok {
		return login, nil
	}
	return "", fmt.Errorf("github app: no such installation %d", id)
}

func (f *fakeGitHubApp) InstallURL(context.Context) (string, error) {
	if f.installURL == "" {
		return "", fmt.Errorf("github app: GET /app failed")
	}
	return f.installURL, nil
}

// githubAppFixture bundles the two forge doubles: credsForge receives
// clients built from stored credentials, app.appForge the ones built
// from the installation.
type githubAppFixture struct {
	*fixture
	app       *fakeGitHubApp
	appForge  *forgefake.Forge
	workspace *store.Workspace
}

// newGitHubAppFixture builds a GitHub-forge server with the App
// configured, workspace acme (created unless hosted claims it later) and
// a signed-in member whose forge list is [acme janedev].
func newGitHubAppFixture(t *testing.T, hosted, withWorkspace bool) (*githubAppFixture, *http.Cookie) {
	t.Helper()
	st := storemem.New()
	var ws *store.Workspace
	if withWorkspace {
		ws = &store.Workspace{Forge: "github", Prefix: "acme", Token: "ws-secret", DefaultBranch: "main"}
		if err := st.CreateWorkspace(context.Background(), ws); err != nil {
			t.Fatal(err)
		}
	}
	credsForge := forgefake.New()
	appForge := forgefake.New()
	app := &fakeGitHubApp{
		appForge:   appForge,
		accounts:   map[int64]string{42: "acme"},
		installURL: "https://github.com/apps/gocov/installations/new",
	}
	provider := &fakeProvider{name: "github", identity: &auth.Identity{
		ForgeUUID:   "{uuid-gh-1}",
		DisplayName: "Jane Dev",
		Email:       "jane@example.com",
		Workspaces:  []string{"acme", "janedev"},
	}}
	f := &githubAppFixture{
		fixture: &fixture{
			srv: New(Config{
				Store:     st,
				Blobs:     blobmem.New(),
				Parsers:   map[string]profile.Parser{"go": profile.GoParser{}},
				Forges:    map[string]forge.Factory{"github": credsForge.Factory()},
				BaseURL:   "https://gocov.example",
				Auths:     []auth.Provider{provider},
				Hosted:    hosted,
				GitHubApp: app,
			}),
			store: st,
			forge: credsForge,
		},
		app:       app,
		appForge:  appForge,
		workspace: ws,
	}
	return f, signInVia(t, f.fixture, "github")
}

// signInVia drives the OAuth flow for the named forge (signIn is
// bitbucket-only).
func signInVia(t *testing.T, f *fixture, forgeName string) *http.Cookie {
	t.Helper()
	start := get(f, "/oauth/"+forgeName+"/start?next=%2F")
	if start.Code != http.StatusFound {
		t.Fatalf("start: status = %d", start.Code)
	}
	stateCk := cookieNamed(t, start, stateCookie)
	state, _, _ := strings.Cut(stateCk.Value, "|")
	cb := get(f, "/oauth/"+forgeName+"/callback?code=thecode&state="+url.QueryEscape(state), stateCk)
	if cb.Code != http.StatusFound {
		t.Fatalf("callback: status = %d", cb.Code)
	}
	return cookieNamed(t, cb, sessionCookie)
}

func (f *githubAppFixture) reloadWorkspace(t *testing.T, prefix string) *store.Workspace {
	t.Helper()
	ws, err := f.store.WorkspaceByPrefix(context.Background(), prefix)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func (f *githubAppFixture) connectWorkspace(t *testing.T, installationID int64) {
	t.Helper()
	ws := f.reloadWorkspace(t, "acme")
	ws.GitHubInstallationID = installationID
	if err := f.store.UpdateWorkspace(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubSetupConnectsWorkspace(t *testing.T) {
	f, sess := newGitHubAppFixture(t, false, true)

	rec := get(f.fixture, "/github/setup?installation_id=42&setup_action=install", sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/workspaces/acme?connected=1" {
		t.Errorf("redirect = %q", loc)
	}
	ws := f.reloadWorkspace(t, "acme")
	if ws.GitHubInstallationID != 42 {
		t.Errorf("installation id = %d, want 42", ws.GitHubInstallationID)
	}

	// The settings page renders the connected state and the notice.
	page := get(f.fixture, "/workspaces/acme?connected=1", sess)
	body := page.Body.String()
	if !strings.Contains(body, "connected") || !strings.Contains(body, "gocov[bot]") {
		t.Error("settings page must show the connected GitHub App state")
	}
	if !strings.Contains(body, "Disconnect") {
		t.Error("settings page must offer disconnect while connected")
	}
}

func TestGitHubSetupReconnectClearsBroken(t *testing.T) {
	f, sess := newGitHubAppFixture(t, false, true)
	ws := f.reloadWorkspace(t, "acme")
	ws.GitHubInstallationID = 41
	ws.GitHubAppBroken = true
	if err := f.store.UpdateWorkspace(context.Background(), ws); err != nil {
		t.Fatal(err)
	}

	if rec := get(f.fixture, "/github/setup?installation_id=42", sess); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	ws = f.reloadWorkspace(t, "acme")
	if ws.GitHubInstallationID != 42 || ws.GitHubAppBroken {
		t.Errorf("after reconnect: id = %d, broken = %v; want 42, false", ws.GitHubInstallationID, ws.GitHubAppBroken)
	}
}

func TestGitHubSetupRejectsForeignInstallation(t *testing.T) {
	// The API says the installation lives on an account the user has no
	// relationship with — neither membership nor forge list. An existing
	// workspace must not be connectable (403), and a hosted claim must
	// not be grantable (403) — the installation_id query parameter alone
	// proves nothing.
	f, sess := newGitHubAppFixture(t, true, true)
	foreign := &store.Workspace{Forge: "github", Prefix: "evilcorp", Token: "evil-tok", DefaultBranch: "main"}
	if err := f.store.CreateWorkspace(context.Background(), foreign); err != nil {
		t.Fatal(err)
	}
	f.app.accounts[7] = "evilcorp"

	if rec := get(f.fixture, "/github/setup?installation_id=7", sess); rec.Code != http.StatusForbidden {
		t.Fatalf("existing foreign workspace: status = %d, want 403", rec.Code)
	}
	if ws := f.reloadWorkspace(t, "evilcorp"); ws.GitHubInstallationID != 0 {
		t.Error("foreign installation must not connect anything")
	}

	// Same story for the claim path: the account is unregistered, but
	// the user's forge list does not vouch for it.
	f.app.accounts[8] = "strangers"
	rec := get(f.fixture, "/github/setup?installation_id=8", sess)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign claim: status = %d, want 403", rec.Code)
	}
	// The dead end offers two ways forward: a link to the org's OAuth-app
	// policy (the usual cause — a restricted org gocov can't see), and a
	// one-click re-auth back to this same install for the stale-snapshot case.
	if !strings.Contains(rec.Body.String(), "Sign in again") ||
		!strings.Contains(rec.Body.String(), "/oauth/github/start?next=") ||
		!strings.Contains(rec.Body.String(), "installation_id%3D8") {
		t.Errorf("claim-denied page missing the re-auth self-heal link:\n%s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "github.com/organizations/strangers/settings/oauth_application_policy") {
		t.Errorf("claim-denied page missing the org OAuth-policy link:\n%s", rec.Body)
	}
	if _, err := f.store.WorkspaceByPrefix(context.Background(), "strangers"); err == nil {
		t.Error("foreign claim must not register a workspace")
	}
}

func TestGitHubSetupBadRequests(t *testing.T) {
	f, sess := newGitHubAppFixture(t, false, true)

	if rec := get(f.fixture, "/github/setup", sess); rec.Code != http.StatusBadRequest {
		t.Errorf("no installation_id: status = %d, want 400", rec.Code)
	}
	if rec := get(f.fixture, "/github/setup?installation_id=abc", sess); rec.Code != http.StatusBadRequest {
		t.Errorf("bad installation_id: status = %d, want 400", rec.Code)
	}
	// GitHub cannot confirm the installation (unknown id).
	if rec := get(f.fixture, "/github/setup?installation_id=99", sess); rec.Code != http.StatusBadGateway {
		t.Errorf("unknown installation: status = %d, want 502", rec.Code)
	}
	// An install request by a non-admin member: informational page.
	if rec := get(f.fixture, "/github/setup?setup_action=request", sess); rec.Code != http.StatusOK {
		t.Errorf("setup_action=request: status = %d, want 200", rec.Code)
	}
}

func TestGitHubSetupWithoutApp404s(t *testing.T) {
	// No App configured: the route does not exist, like every feature
	// switch on this server.
	f, sess := newWorkspaceFixture(t, nil, false)
	if rec := get(f, "/github/setup?installation_id=42", sess); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGitHubSetupClaimsWorkspaceHosted(t *testing.T) {
	// Install-first onboarding: no workspace registered yet; the account
	// is vouched for by the user's forge list, so hosted mode claims it
	// with the installation already linked (M3 claim rules).
	f, sess := newGitHubAppFixture(t, true, false)
	f.app.accounts[7] = "janedev"

	rec := get(f.fixture, "/github/setup?installation_id=7", sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/onboarding?ws=janedev" {
		t.Errorf("redirect = %q, want the workspace-ready state (activation moment)", loc)
	}
	ws := f.reloadWorkspace(t, "janedev")
	if ws.GitHubInstallationID != 7 || ws.Forge != "github" {
		t.Errorf("claimed workspace: installation = %d, forge = %q", ws.GitHubInstallationID, ws.Forge)
	}
	// The registering user must be a member (RegisterWorkspace semantics).
	if rec := get(f.fixture, "/workspaces/janedev", sess); rec.Code != http.StatusOK {
		t.Errorf("claimer cannot open the settings page: status = %d", rec.Code)
	}
}

func TestGitHubSetupClaimPrivateMode(t *testing.T) {
	// A private instance has no self-service registration; installs on
	// unregistered accounts point the admin at the CLI.
	f, sess := newGitHubAppFixture(t, false, true)
	f.app.accounts[7] = "janedev"

	if rec := get(f.fixture, "/github/setup?installation_id=7", sess); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if _, err := f.store.WorkspaceByPrefix(context.Background(), "janedev"); err == nil {
		t.Error("private mode must not register workspaces")
	}
}

func TestGitHubDisconnect(t *testing.T) {
	f, sess := newGitHubAppFixture(t, false, true)
	f.connectWorkspace(t, 42)

	rec := postForm(f.fixture, "/workspaces/acme/github/disconnect", url.Values{}, sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if ws := f.reloadWorkspace(t, "acme"); ws.GitHubInstallationID != 0 || ws.GitHubAppBroken {
		t.Errorf("after disconnect: id = %d, broken = %v", ws.GitHubInstallationID, ws.GitHubAppBroken)
	}
}

// uploadRepo creates a GitHub repo under acme with optional credentials.
func (f *githubAppFixture) uploadRepo(t *testing.T, creds map[string]string) {
	t.Helper()
	repo := &store.Repo{Forge: "github", Slug: "acme/widgets", Token: "repo-token",
		DefaultBranch: "main", ForgeCredentials: creds}
	if err := f.store.CreateRepo(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	f.repo = repo
}

func uploadResp(t *testing.T, rec *httptest.ResponseRecorder) uploadResponse {
	t.Helper()
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestUploadPrefersInstallation(t *testing.T) {
	// D4: the installation outranks even per-repo credentials, so the
	// check-run path always runs as the App where one is connected.
	f, _ := newGitHubAppFixture(t, false, true)
	f.connectWorkspace(t, 42)
	f.uploadRepo(t, map[string]string{"token": "pat"})

	rec := doUpload(t, f.fixture, "repo-token", map[string]string{
		"repo": "acme/widgets", "commit": "abc123",
	}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	resp := uploadResp(t, rec)
	if resp.BuildStatus != "posted" || resp.CodeInsights != "posted" {
		t.Errorf("status/insights = %q/%q, want posted/posted", resp.BuildStatus, resp.CodeInsights)
	}
	if len(f.appForge.StatusCalls) != 1 || len(f.appForge.ReportCalls) != 1 {
		t.Errorf("app forge got %d status / %d report calls, want 1/1",
			len(f.appForge.StatusCalls), len(f.appForge.ReportCalls))
	}
	if got := len(f.forge.FactoryCreds); got != 0 {
		t.Errorf("credential factory ran %d times, want 0 (installation outranks repo creds)", got)
	}
}

func TestUploadRevokedInstallationDegrades(t *testing.T) {
	// Uninstall, detected lazily (D3): the mint fails, the workspace is
	// flagged, and the upload degrades exactly like missing credentials.
	f, _ := newGitHubAppFixture(t, false, true)
	f.connectWorkspace(t, 42)
	f.uploadRepo(t, nil)
	f.app.forgeErr = fmt.Errorf("%w: installation 42 gone", forge.ErrCredentialsRevoked)

	rec := doUpload(t, f.fixture, "repo-token", map[string]string{
		"repo": "acme/widgets", "commit": "abc123",
	}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s (an uninstall must never fail the upload)", rec.Code, rec.Body)
	}
	resp := uploadResp(t, rec)
	if resp.BuildStatus != "skipped" || resp.CodeInsights != "skipped" {
		t.Errorf("status/insights = %q/%q, want skipped/skipped", resp.BuildStatus, resp.CodeInsights)
	}
	ws := f.reloadWorkspace(t, "acme")
	if !ws.GitHubAppBroken {
		t.Error("revoked mint must flag the workspace broken")
	}
	if ws.GitHubInstallationID != 42 {
		t.Error("the installation link is flagged, not erased")
	}
}

func TestUploadRevokedInstallationFallsBackToCreds(t *testing.T) {
	// With stored credentials further down the chain, a broken App
	// degrades to them — PAT-configured repos behave as before.
	f, _ := newGitHubAppFixture(t, false, true)
	f.connectWorkspace(t, 42)
	f.uploadRepo(t, map[string]string{"token": "pat"})
	f.app.forgeErr = fmt.Errorf("%w: installation 42 gone", forge.ErrCredentialsRevoked)

	rec := doUpload(t, f.fixture, "repo-token", map[string]string{
		"repo": "acme/widgets", "commit": "abc123",
	}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if resp := uploadResp(t, rec); resp.BuildStatus != "posted" {
		t.Errorf("build status = %q, want posted via the repo credential", resp.BuildStatus)
	}
	if len(f.forge.StatusCalls) != 1 {
		t.Errorf("credential forge got %d status calls, want 1", len(f.forge.StatusCalls))
	}
}

func TestUploadHealsBrokenFlag(t *testing.T) {
	f, _ := newGitHubAppFixture(t, false, true)
	f.connectWorkspace(t, 42)
	ws := f.reloadWorkspace(t, "acme")
	ws.GitHubAppBroken = true
	if err := f.store.UpdateWorkspace(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	f.uploadRepo(t, nil)

	if rec := doUpload(t, f.fixture, "repo-token", map[string]string{
		"repo": "acme/widgets", "commit": "abc123",
	}, testProfile); rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if ws := f.reloadWorkspace(t, "acme"); ws.GitHubAppBroken {
		t.Error("a working mint must clear the broken flag")
	}
}

func TestUploadWorkspaceTokenUsesInstallation(t *testing.T) {
	// Auto-registration through a workspace token asks the App for the
	// default branch — the zero-credential acceptance path.
	f, _ := newGitHubAppFixture(t, false, true)
	f.connectWorkspace(t, 42)
	f.appForge.DefaultBranch = "trunk"

	rec := doUpload(t, f.fixture, "ws-secret", map[string]string{
		"repo": "acme/newrepo", "commit": "abc123",
	}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	resp := uploadResp(t, rec)
	if !resp.RepoCreated {
		t.Error("repo must auto-register")
	}
	if resp.BuildStatus != "posted" {
		t.Errorf("build status = %q, want posted through the installation", resp.BuildStatus)
	}
	repo, err := f.store.RepoBySlug(context.Background(), "acme/newrepo")
	if err != nil {
		t.Fatal(err)
	}
	if repo.DefaultBranch != "trunk" {
		t.Errorf("default branch = %q, want asked from the App client", repo.DefaultBranch)
	}
}

func TestSettingsPageBrokenState(t *testing.T) {
	f, sess := newGitHubAppFixture(t, false, true)
	f.connectWorkspace(t, 42)
	ws := f.reloadWorkspace(t, "acme")
	ws.GitHubAppBroken = true
	if err := f.store.UpdateWorkspace(context.Background(), ws); err != nil {
		t.Fatal(err)
	}

	body := get(f.fixture, "/workspaces/acme", sess).Body.String()
	if !strings.Contains(body, "reconnect needed") {
		t.Error("settings page must surface the broken connection")
	}
	if !strings.Contains(body, f.app.installURL) {
		t.Error("broken state must link the reinstall page")
	}
}

func TestSetupPageRecommendsApp(t *testing.T) {
	f, sess := newGitHubAppFixture(t, true, true)

	// The reporting capability lives in the Workspace step's ready state.
	body := get(f.fixture, "/onboarding?ws=acme", sess).Body.String()
	if !strings.Contains(body, "Grant write access") || !strings.Contains(body, f.app.installURL) {
		t.Error("ready state must offer the App grant while not connected")
	}

	f.connectWorkspace(t, 42)
	body = get(f.fixture, "/onboarding?ws=acme", sess).Body.String()
	if strings.Contains(body, "Grant write access") {
		t.Error("ready state must drop the grant once connected")
	}
	if !strings.Contains(body, "gocov[bot]") {
		t.Error("connected workspace must show the gocov[bot] identity")
	}
}
