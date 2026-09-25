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
	"github.com/gocov/gocov/internal/forge/github"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// fakeGitHubApp is a canned server.GitHubApp: installations resolve via
// the accounts map, ForgeClient hands out appForge (or forgeErr), and
// verify answers VerifyRunClaim (nil verify accepts every claim).
type fakeGitHubApp struct {
	appForge    forge.Forge
	forgeErr    error
	accounts    map[int64]string
	installURL  string
	forgeCalls  []int64
	verify      func(installationID int64, claim github.RunClaim) error
	verifyCalls []github.RunClaim
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

func (f *fakeGitHubApp) VerifyRunClaim(_ context.Context, installationID int64, claim github.RunClaim) error {
	f.verifyCalls = append(f.verifyCalls, claim)
	if f.verify == nil {
		return nil
	}
	return f.verify(installationID, claim)
}

// githubAppFixture exposes the App's forge double (app.appForge, also the
// fixture's forge): the client built from the installation.
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
		if err := st.CreateWorkspace(t.Context(), ws); err != nil {
			t.Fatal(err)
		}
	}
	appForge := forgefake.New()
	app := &fakeGitHubApp{
		appForge:   appForge,
		accounts:   map[int64]string{42: "acme"},
		installURL: "https://github.com/apps/gocov/installations/new",
	}
	provider := &fakeProvider{name: "github", identity: &auth.Identity{
		ForgeUUID:       "{uuid-gh-1}",
		DisplayName:     "Jane Dev",
		Email:           "jane@example.com",
		Workspaces:      []string{"acme", "janedev"},
		OwnedWorkspaces: []string{"acme", "janedev"},
	}}
	f := &githubAppFixture{
		fixture: &fixture{
			srv: New(Config{
				Store:     st,
				Blobs:     blobmem.New(),
				BaseURL:   "https://gocov.example",
				Auths:     []auth.Provider{provider},
				Hosted:    hosted,
				GitHubApp: app,
			}),
			store: st,
			forge: appForge,
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
	ws, err := f.store.WorkspaceByPrefix(t.Context(), "github", prefix)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func (f *githubAppFixture) connectWorkspace(t *testing.T, installationID int64) {
	t.Helper()
	ws := f.reloadWorkspace(t, "acme")
	ws.GitHubInstallationID = installationID
	if err := f.store.UpdateWorkspace(t.Context(), ws); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubSetupConnectsWorkspace(t *testing.T) {
	f, sess := newGitHubAppFixture(t, false, true)

	rec := get(f.fixture, "/github/setup?installation_id=42&setup_action=install", sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	// A connected workspace's home is its dashboard, where the setup
	// checklist reads the connection it just gained.
	if loc := rec.Header().Get("Location"); loc != "/w/github/acme" {
		t.Errorf("redirect = %q, want the workspace's dashboard", loc)
	}
	ws := f.reloadWorkspace(t, "acme")
	if ws.GitHubInstallationID != 42 {
		t.Errorf("installation id = %d, want 42", ws.GitHubInstallationID)
	}

	// The settings screen reads the connection as on, posting as gocov[bot].
	got := decodeJSON[workspaceSettingsDTO](t, get(f.fixture, "/api/ui/workspace-settings/github/acme", sess))
	if got.Reporting.State != "on" {
		t.Errorf("reporting = %+v, want the App connected", got.Reporting)
	}
}

func TestGitHubSetupReconnectClearsBroken(t *testing.T) {
	f, sess := newGitHubAppFixture(t, false, true)
	ws := f.reloadWorkspace(t, "acme")
	ws.GitHubInstallationID = 41
	ws.GitHubAppBroken = true
	if err := f.store.UpdateWorkspace(t.Context(), ws); err != nil {
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
	if err := f.store.CreateWorkspace(t.Context(), foreign); err != nil {
		t.Fatal(err)
	}
	f.app.accounts[7] = "evilcorp"

	wantConnectOutcome(t, get(f.fixture, "/github/setup?installation_id=7", sess), "not_your_workspace", "evilcorp")
	if ws := f.reloadWorkspace(t, "evilcorp"); ws.GitHubInstallationID != 0 {
		t.Error("foreign installation must not connect anything")
	}

	// Same story for the claim path: the account is unregistered, but
	// the user's forge list does not vouch for it.
	f.app.accounts[8] = "strangers"
	// The dead end carries what the app needs to offer both ways forward:
	// the org's OAuth-app policy page (the usual cause — a restricted org
	// gocov can't see) and a re-auth back to this same install for the
	// stale-snapshot case.
	q := wantConnectOutcome(t, get(f.fixture, "/github/setup?installation_id=8", sess), "not_your_workspace", "strangers")
	if q.Get("installation_id") != "8" {
		t.Errorf("denial query = %v, want the installation to come back to", q)
	}
	if _, err := f.store.WorkspaceByPrefix(t.Context(), "github", "strangers"); err == nil {
		t.Error("foreign claim must not register a workspace")
	}
}

func TestGitHubSetupBadRequests(t *testing.T) {
	f, sess := newGitHubAppFixture(t, false, true)

	wantConnectOutcome(t, get(f.fixture, "/github/setup", sess), "no_installation", "")
	wantConnectOutcome(t, get(f.fixture, "/github/setup?installation_id=abc", sess), "no_installation", "")
	// GitHub cannot confirm the installation (unknown id).
	wantConnectOutcome(t, get(f.fixture, "/github/setup?installation_id=99", sess), "install_unconfirmed", "")
	// An install request by a non-admin member: nothing to link yet.
	wantConnectOutcome(t, get(f.fixture, "/github/setup?setup_action=request", sess), "install_requested", "")
}

func TestGitHubSetupWithoutApp404s(t *testing.T) {
	// No App configured: the route does not exist, like every feature
	// switch on this server.
	f, sess := newWorkspaceFixture(t, false)
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
	if loc := rec.Header().Get("Location"); loc != "/w/github/janedev" {
		t.Errorf("redirect = %q, want the new workspace's dashboard (activation moment)", loc)
	}
	ws := f.reloadWorkspace(t, "janedev")
	if ws.GitHubInstallationID != 7 || ws.Forge != "github" {
		t.Errorf("claimed workspace: installation = %d, forge = %q", ws.GitHubInstallationID, ws.Forge)
	}
	// The registering user must be a member (RegisterWorkspace semantics).
	if rec := get(f.fixture, "/api/ui/workspace-settings/github/janedev", sess); rec.Code != http.StatusOK {
		t.Errorf("claimer cannot read the workspace: status = %d", rec.Code)
	}
}

func TestGitHubSetupClaimPrivateMode(t *testing.T) {
	// Install-first onboarding works in private mode too: the account is
	// vouched for by the signed-in user's forge list, so the install claims
	// it with the installation linked — the same claim rules as hosted, the
	// forge-list check being the only gate.
	f, sess := newGitHubAppFixture(t, false, true)
	f.app.accounts[7] = "janedev"

	rec := get(f.fixture, "/github/setup?installation_id=7", sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/w/github/janedev" {
		t.Errorf("redirect = %q, want the new workspace's dashboard", loc)
	}
	ws, err := f.store.WorkspaceByPrefix(t.Context(), "github", "janedev")
	if err != nil {
		t.Fatalf("private-mode install must register the workspace: %v", err)
	}
	if ws.GitHubInstallationID != 7 || ws.Forge != "github" {
		t.Errorf("claimed workspace: installation = %d, forge = %q", ws.GitHubInstallationID, ws.Forge)
	}
}

func TestGitHubSetupClaimBesideAnotherForgesNamesake(t *testing.T) {
	// Names are scoped per forge: installing on the GitHub org "acme" next
	// to a registered Bitbucket workspace "acme" creates github/acme and
	// leaves the Bitbucket tenant alone — the old global namespace turned
	// the install away with a 409.
	f, sess := newGitHubAppFixture(t, true, false)
	bb := &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "bb-secret", DefaultBranch: "main"}
	if err := f.store.CreateWorkspace(t.Context(), bb); err != nil {
		t.Fatal(err)
	}

	rec := get(f.fixture, "/github/setup?installation_id=42", sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	gh := f.reloadWorkspace(t, "acme")
	if gh.ID == bb.ID || gh.GitHubInstallationID != 42 {
		t.Errorf("github acme = %+v, want a new workspace linked to installation 42", gh)
	}
	if got, err := f.store.WorkspaceByPrefix(t.Context(), "bitbucket", "acme"); err != nil || got.Token != "bb-secret" || got.GitHubInstallationID != 0 {
		t.Errorf("bitbucket acme changed: %+v, %v", got, err)
	}
}

func TestGitHubSetupClaimDeniedNonMember(t *testing.T) {
	// The forge-list check still gates: an install on an account the user's
	// forge identity does not vouch for is refused, in private mode as in
	// hosted.
	f, sess := newGitHubAppFixture(t, false, true)
	f.app.accounts[7] = "stranger" // not in the identity's Workspaces

	wantConnectOutcome(t, get(f.fixture, "/github/setup?installation_id=7", sess), "not_your_workspace", "stranger")
	if _, err := f.store.WorkspaceByPrefix(t.Context(), "github", "stranger"); err == nil {
		t.Error("an unvouched account must not be registered")
	}
}

func TestGitHubSetupIsOwnersOnly(t *testing.T) {
	// Linking an installation to a tracked workspace is an owner's move.
	// A member arriving with one — most likely an admin whose gocov role
	// predates the promotion — is told to sign in again, and nothing links.
	f, sess := newGitHubAppFixture(t, false, true)
	demote(t, f.fixture, "acme")

	q := wantConnectOutcome(t, get(f.fixture, "/github/setup?installation_id=42", sess), "owners_only", "acme")
	if q.Get("installation_id") != "42" {
		t.Errorf("denial query = %v, want the installation to come back to after a re-auth", q)
	}
	if ws := f.reloadWorkspace(t, "acme"); ws.GitHubInstallationID != 0 {
		t.Errorf("installation linked by a member: %d", ws.GitHubInstallationID)
	}
	if rec := postJSON(t, f.fixture, "/api/ui/workspace-settings/disconnect/github/acme", nil, sess); rec.Code != http.StatusForbidden {
		t.Errorf("member disconnect: status = %d, want 403", rec.Code)
	}
}

func TestGitHubSetupClaimNeedsAnOwnerOnTheForge(t *testing.T) {
	// Install-first onboarding creates the workspace, so it takes the
	// forge's admin role on the org — the same rule as /register.
	f, sess := newGitHubAppFixture(t, true, false)
	f.app.accounts[7] = "janedev"
	users, _ := f.store.ListUsers(t.Context())
	users[0].ForgeOwnedWorkspaces = nil // the forge lists janedev, but not as admin
	if err := f.store.UpsertUser(t.Context(), users[0]); err != nil {
		t.Fatal(err)
	}

	wantConnectOutcome(t, get(f.fixture, "/github/setup?installation_id=7", sess), "owners_only", "janedev")
	if _, err := f.store.WorkspaceByPrefix(t.Context(), "github", "janedev"); err == nil {
		t.Error("a member's install must not register the workspace")
	}
}

func TestGitHubDisconnect(t *testing.T) {
	f, sess := newGitHubAppFixture(t, false, true)
	f.connectWorkspace(t, 42)

	rec := postJSON(t, f.fixture, "/api/ui/workspace-settings/disconnect/github/acme", nil, sess)
	wantStatus(t, rec, "disconnect", http.StatusOK)
	if ws := f.reloadWorkspace(t, "acme"); ws.GitHubInstallationID != 0 || ws.GitHubAppBroken {
		t.Errorf("after disconnect: id = %d, broken = %v", ws.GitHubInstallationID, ws.GitHubAppBroken)
	}
}

// uploadRepo creates a GitHub repo under acme.
func (f *githubAppFixture) uploadRepo(t *testing.T) {
	t.Helper()
	repo := &store.Repo{Forge: "github", Slug: "acme/widgets", Token: "repo-token",
		DefaultBranch: "main"}
	if err := f.store.CreateRepo(t.Context(), repo); err != nil {
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

func TestUploadUsesInstallation(t *testing.T) {
	// D4: the check-run path runs as the App where one is connected.
	f, _ := newGitHubAppFixture(t, false, true)
	f.connectWorkspace(t, 42)
	f.uploadRepo(t)

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
}

func TestUploadRevokedInstallationDegrades(t *testing.T) {
	// Uninstall, detected lazily (D3): the mint fails, the workspace is
	// flagged, and the upload degrades exactly like missing credentials.
	f, _ := newGitHubAppFixture(t, false, true)
	f.connectWorkspace(t, 42)
	f.uploadRepo(t)
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

func TestUploadHealsBrokenFlag(t *testing.T) {
	f, _ := newGitHubAppFixture(t, false, true)
	f.connectWorkspace(t, 42)
	ws := f.reloadWorkspace(t, "acme")
	ws.GitHubAppBroken = true
	if err := f.store.UpdateWorkspace(t.Context(), ws); err != nil {
		t.Fatal(err)
	}
	f.uploadRepo(t)

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
	repo, err := f.store.RepoBySlug(t.Context(), "github", "acme/newrepo")
	if err != nil {
		t.Fatal(err)
	}
	if repo.DefaultBranch != "trunk" {
		t.Errorf("default branch = %q, want asked from the App client", repo.DefaultBranch)
	}
}

// The Reporting card, as the settings and setup screens read it: off with
// the install link while unconnected, on once linked, and broken — still
// with the install link, which is the reconnect — when GitHub stops
// answering for the installation.
func TestGitHubReportingStates(t *testing.T) {
	f, sess := newGitHubAppFixture(t, false, true)

	got := decodeJSON[workspaceSettingsDTO](t, get(f.fixture, "/api/ui/workspace-settings/github/acme", sess))
	if !got.Reporting.Available || got.Reporting.State != "off" || got.Reporting.ConnectURL != f.app.installURL {
		t.Errorf("unconnected reporting = %+v, want the install link", got.Reporting)
	}

	f.connectWorkspace(t, 42)
	got = decodeJSON[workspaceSettingsDTO](t, get(f.fixture, "/api/ui/workspace-settings/github/acme", sess))
	if got.Reporting.State != "on" {
		t.Errorf("connected reporting = %+v", got.Reporting)
	}

	ws := f.reloadWorkspace(t, "acme")
	ws.GitHubAppBroken = true
	if err := f.store.UpdateWorkspace(t.Context(), ws); err != nil {
		t.Fatal(err)
	}
	got = decodeJSON[workspaceSettingsDTO](t, get(f.fixture, "/api/ui/workspace-settings/github/acme", sess))
	if got.Reporting.State != "broken" || got.Reporting.ConnectURL != f.app.installURL {
		t.Errorf("broken reporting = %+v, want the reinstall link", got.Reporting)
	}
}

// wantConnectOutcome asserts an install that did not end in a connected
// workspace sends the browser to onboarding, naming what happened and the
// account it was about, and returns the query for further checks.
func wantConnectOutcome(t *testing.T, rec *httptest.ResponseRecorder, outcome, ws string) url.Values {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("connect outcome %q: status = %d, body = %s", outcome, rec.Code, rec.Body)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || loc.Path != "/onboarding" {
		t.Fatalf("connect outcome %q redirected to %q", outcome, rec.Header().Get("Location"))
	}
	q := loc.Query()
	if q.Get("connect") != outcome || q.Get("ws") != ws {
		t.Errorf("connect outcome = %q/%q, want %q/%q", q.Get("connect"), q.Get("ws"), outcome, ws)
	}
	return q
}
