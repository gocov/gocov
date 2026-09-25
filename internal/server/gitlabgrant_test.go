package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/auth"
	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/forge"
	forgefake "github.com/gocov/gocov/internal/forge/fake"
	"github.com/gocov/gocov/internal/forge/gitlab"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// fakeGLConnect is a canned server.GitLabConnect: refreshes hand out
// sequentially numbered rotating grants (or refreshErr), and ForgeClient
// returns grantForge regardless of the token.
type fakeGLConnect struct {
	grantForge   forge.Forge
	refreshErr   error
	refreshCalls []string // refresh tokens received
	redirectURIs []string // redirect URIs received on refresh
	refreshSeq   int
	exchanged    []string // codes received
}

func (f *fakeGLConnect) AuthorizeURL(state, redirectURI string) string {
	return "https://gitlab.example/authorize?state=" + url.QueryEscape(state) +
		"&redirect_uri=" + url.QueryEscape(redirectURI) + "&scope=api"
}

func (f *fakeGLConnect) Exchange(_ context.Context, code, _ string) (*gitlab.Grant, error) {
	f.exchanged = append(f.exchanged, code)
	return &gitlab.Grant{Account: "covbot", AccessToken: "at-0", RefreshToken: "rt-0", TTL: 2 * time.Hour}, nil
}

func (f *fakeGLConnect) Refresh(_ context.Context, refreshToken, redirectURI string) (*gitlab.Grant, error) {
	f.refreshCalls = append(f.refreshCalls, refreshToken)
	f.redirectURIs = append(f.redirectURIs, redirectURI)
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	f.refreshSeq++
	return &gitlab.Grant{
		AccessToken:  fmt.Sprintf("at-%d", f.refreshSeq),
		RefreshToken: fmt.Sprintf("rt-%d", f.refreshSeq),
		TTL:          2 * time.Hour,
	}, nil
}

func (f *fakeGLConnect) ForgeClient(string) forge.Forge { return f.grantForge }

type glConnectFixture struct {
	*fixture
	gl         *fakeGLConnect
	grantForge *forgefake.Forge
}

// newGLConnectFixture builds a gitlab-forge server with connect enabled,
// the subgroup workspace grp/sub (its prefix rides URL-encoded in every
// route) and a signed-in member.
func newGLConnectFixture(t *testing.T) (*glConnectFixture, *http.Cookie) {
	t.Helper()
	st := storemem.New()
	ws := &store.Workspace{Forge: "gitlab", Prefix: "grp/sub", Token: "ws-secret", DefaultBranch: "main"}
	if err := st.CreateWorkspace(t.Context(), ws); err != nil {
		t.Fatal(err)
	}
	grantForge := forgefake.New()
	gl := &fakeGLConnect{grantForge: grantForge}
	f := &glConnectFixture{
		fixture: &fixture{
			srv: New(Config{
				Store:   st,
				Blobs:   blobmem.New(),
				Parsers: map[string]profile.Parser{"go": profile.GoParser{}},
				BaseURL: "https://gocov.example",
				Hosted:  true,
				Auths: []auth.Provider{&fakeProvider{name: "gitlab", identity: &auth.Identity{
					ForgeUUID: "777", DisplayName: "Jane Dev", Email: "jane@example.com",
					Workspaces:      []string{"grp/sub", "janedev"},
					OwnedWorkspaces: []string{"grp/sub", "janedev"},
				}}},
				GitLabConnect: gl,
			}),
			store: st,
			forge: grantForge,
		},
		gl:         gl,
		grantForge: grantForge,
	}
	return f, signInVia(t, f.fixture, "gitlab")
}

func (f *glConnectFixture) workspace(t *testing.T) *store.Workspace {
	t.Helper()
	ws, err := f.store.WorkspaceByPrefix(t.Context(), "gitlab", "grp/sub")
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func (f *glConnectFixture) grant(t *testing.T, account, refresh string, broken bool) {
	t.Helper()
	ws := f.workspace(t)
	if err := f.store.SetWorkspaceGrant(t.Context(), ws.ID, "gitlab", store.Grant{Account: account, RefreshToken: refresh, Broken: broken}); err != nil {
		t.Fatal(err)
	}
}

func (f *glConnectFixture) addRepo(t *testing.T) {
	t.Helper()
	repo := &store.Repo{Forge: "gitlab", Slug: "grp/sub/proj", Token: "repo-token",
		DefaultBranch: "main"}
	if err := f.store.CreateRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	f.repo = repo
}

func (f *glConnectFixture) upload(t *testing.T) uploadResponse {
	t.Helper()
	rec := doUpload(t, f.fixture, "repo-token", map[string]string{
		"repo": "grp/sub/proj", "commit": "abc123",
	}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", rec.Code, rec.Body)
	}
	return uploadResp(t, rec)
}

func TestGitLabConnectFlow(t *testing.T) {
	f, sess := newGLConnectFixture(t)

	start := get(f.fixture, "/workspace-connect/gitlab/grp/sub", sess)
	if start.Code != http.StatusFound {
		t.Fatalf("connect start: status = %d", start.Code)
	}
	loc, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	// The connect consent reuses the sign-in callback — GitLab enforces
	// an exact match against the application's registered redirect URIs.
	if got := loc.Query().Get("redirect_uri"); got != "https://gocov.example/oauth/gitlab/callback" {
		t.Errorf("redirect_uri = %q, must equal the sign-in callback exactly", got)
	}
	stateCk := cookieNamed(t, start, glConnectStateCookie)
	state, prefix := splitConnectState(stateCk.Value)
	if prefix != "grp/sub" {
		t.Errorf("cookie prefix = %q", prefix)
	}

	cb := get(f.fixture, "/oauth/gitlab/callback?code=thecode&state="+url.QueryEscape(state), stateCk, sess)
	if cb.Code != http.StatusSeeOther {
		t.Fatalf("callback: status = %d, body = %s", cb.Code, cb.Body)
	}
	// The nested prefix comes back as the path it is, slashes and all.
	if loc := cb.Header().Get("Location"); loc != "/w/gitlab/grp/sub" {
		t.Errorf("callback redirect = %q, want the workspace's dashboard", loc)
	}
	ws := f.workspace(t)
	if ws.Grant.Account != "covbot" || ws.Grant.RefreshToken != "rt-0" || ws.Grant.Broken {
		t.Errorf("stored grant = %q/%q/broken=%v", ws.Grant.Account, ws.Grant.RefreshToken, ws.Grant.Broken)
	}
}

func TestGitLabConnectCallbackRejects(t *testing.T) {
	f, sess := newGLConnectFixture(t)
	mk := func(value string) *http.Cookie {
		return &http.Cookie{Name: glConnectStateCookie, Value: value}
	}

	// State mismatch: not recognizably a connect return — falls through
	// to the sign-in callback flow, which rejects it its own way.
	if rec := get(f.fixture, "/oauth/gitlab/callback?code=x&state=other", mk("state|grp/sub"), sess); rec.Code != http.StatusFound ||
		rec.Header().Get("Location") != "/login?error=1" {
		t.Errorf("state mismatch: %d -> %q, want the sign-in flow's failure redirect", rec.Code, rec.Header().Get("Location"))
	}
	// No session: back to sign-in, aimed at the settings page the Connect
	// button sits on.
	if rec := get(f.fixture, "/oauth/gitlab/callback?code=x&state=s", mk("s|grp/sub")); rec.Code != http.StatusSeeOther ||
		rec.Header().Get("Location") != "/login?next=%2Fworkspace-settings%2Fgitlab%2Fgrp%2Fsub" {
		t.Errorf("no session: %d -> %q, want the login redirect back to the settings page",
			rec.Code, rec.Header().Get("Location"))
	}
	// A workspace the user is no member of.
	if err := f.store.CreateWorkspace(t.Context(),
		&store.Workspace{Forge: "gitlab", Prefix: "beta", Token: "beta-tok", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if rec := get(f.fixture, "/oauth/gitlab/callback?code=x&state=s", mk("s|beta"), sess); rec.Code != http.StatusSeeOther ||
		rec.Header().Get("Location") != "/?error=connect_denied" {
		t.Errorf("non-member workspace: %d -> %q, want the dashboard's connect_denied notice", rec.Code, rec.Header().Get("Location"))
	}
	// A workspace that is not there reads exactly the same (D3).
	if rec := get(f.fixture, "/oauth/gitlab/callback?code=x&state=s", mk("s|nowhere"), sess); rec.Code != http.StatusSeeOther ||
		rec.Header().Get("Location") != "/?error=connect_denied" {
		t.Errorf("missing workspace: %d -> %q, want the same answer as a non-member", rec.Code, rec.Header().Get("Location"))
	}
	// An owner demoted while the consent was open: still a member, so the
	// settings page says whose move connecting is.
	demote(t, f.fixture, "grp/sub")
	if rec := get(f.fixture, "/oauth/gitlab/callback?code=x&state=s", mk("s|grp/sub"), sess); rec.Code != http.StatusSeeOther ||
		rec.Header().Get("Location") != "/workspace-settings/gitlab/grp/sub?error=connect_owners_only" {
		t.Errorf("demoted owner: %d -> %q, want the settings page's connect_owners_only notice", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.gl.exchanged) != 0 {
		t.Errorf("rejected callbacks must not exchange codes; exchanged %v", f.gl.exchanged)
	}
}

func TestGitLabConnectRequiresFeature(t *testing.T) {
	// No GitLabConnect configured: the connect start does not exist, and
	// a stray connect cookie on the sign-in callback changes nothing.
	f := &fixture{srv: New(Config{
		Store:   storemem.New(),
		Blobs:   blobmem.New(),
		Parsers: map[string]profile.Parser{"go": profile.GoParser{}},
		BaseURL: "https://gocov.example",
		Auths:   []auth.Provider{&fakeProvider{name: "gitlab", identity: &auth.Identity{ForgeUUID: "1", Workspaces: []string{"grp"}}}},
		Hosted:  true,
	})}
	sess := signInVia(t, f, "gitlab")
	if rec := get(f, "/workspace-connect/gitlab/grp", sess); rec.Code != http.StatusNotFound {
		t.Errorf("connect without feature: status = %d, want 404", rec.Code)
	}
	stray := &http.Cookie{Name: glConnectStateCookie, Value: "s|grp"}
	if rec := get(f, "/oauth/gitlab/callback?code=x&state=s", stray); rec.Code != http.StatusFound ||
		rec.Header().Get("Location") != "/login?error=1" {
		t.Errorf("callback without feature: %d -> %q, want the sign-in flow's failure redirect",
			rec.Code, rec.Header().Get("Location"))
	}
}

func TestGitLabUploadUsesGrantAndPersistsRotation(t *testing.T) {
	// The grant serves the upload, and the rotated refresh token replaces
	// the stored one on the first refresh.
	f, _ := newGLConnectFixture(t)
	f.grant(t, "covbot", "rt-0", false)
	f.addRepo(t)

	resp := f.upload(t)
	if resp.BuildStatus != "posted" {
		t.Errorf("build status = %q, want posted", resp.BuildStatus)
	}
	if len(f.grantForge.StatusCalls) != 1 {
		t.Errorf("grant forge got %d status calls, want 1", len(f.grantForge.StatusCalls))
	}
	if got := f.gl.refreshCalls; len(got) != 1 || got[0] != "rt-0" {
		t.Errorf("refresh calls = %v, want exactly the stored token", got)
	}
	// GitLab wants the redirect URI on the refresh grant too.
	if got := f.gl.redirectURIs; len(got) != 1 || got[0] != "https://gocov.example/oauth/gitlab/callback" {
		t.Errorf("refresh redirect URIs = %v", got)
	}
	if ws := f.workspace(t); ws.Grant.RefreshToken != "rt-1" {
		t.Errorf("stored refresh = %q, want the rotated rt-1", ws.Grant.RefreshToken)
	}
}

func TestGitLabUploadGrantAccessTokenCached(t *testing.T) {
	f, _ := newGLConnectFixture(t)
	f.grant(t, "covbot", "rt-0", false)
	f.addRepo(t)

	f.upload(t)
	f.upload(t)
	if got := len(f.gl.refreshCalls); got != 1 {
		t.Errorf("refresh ran %d times for 2 uploads, want 1 (2h token cached)", got)
	}
}

func TestGitLabUploadGrantRevokedDegrades(t *testing.T) {
	f, _ := newGLConnectFixture(t)
	f.grant(t, "covbot", "rt-0", false)
	f.addRepo(t)
	f.gl.refreshErr = fmt.Errorf("%w: invalid_grant", forge.ErrCredentialsRevoked)

	resp := f.upload(t)
	if resp.BuildStatus != "skipped" {
		t.Errorf("build status = %q, want skipped", resp.BuildStatus)
	}
	ws := f.workspace(t)
	if !ws.Grant.Broken {
		t.Error("revoked refresh must flag the grant broken")
	}
	if ws.Grant.Account != "covbot" {
		t.Error("the account name is kept — it says who to replace")
	}
}

func TestGitLabUploadGrantHealsBrokenFlag(t *testing.T) {
	f, _ := newGLConnectFixture(t)
	f.grant(t, "covbot", "rt-0", true)
	f.addRepo(t)

	if resp := f.upload(t); resp.BuildStatus != "posted" {
		t.Fatalf("build status = %q", resp.BuildStatus)
	}
	if ws := f.workspace(t); ws.Grant.Broken {
		t.Error("a working refresh must clear the broken flag")
	}
}

func TestGitLabDisconnect(t *testing.T) {
	f, sess := newGLConnectFixture(t)
	f.grant(t, "covbot", "rt-0", false)

	wantStatus(t, postJSON(t, f.fixture, "/api/ui/workspace-settings/disconnect/gitlab/grp/sub", nil, sess),
		"disconnect", http.StatusOK)
	ws := f.workspace(t)
	if ws.Grant.Account != "" || ws.Grant.RefreshToken != "" || ws.Grant.Broken {
		t.Errorf("after disconnect: %q/%q/%v", ws.Grant.Account, ws.Grant.RefreshToken, ws.Grant.Broken)
	}
}

// The Reporting card, as the settings and setup screens read it: off
// with the consent link while unconnected, on with the account its posts
// carry once granted, and broken — still linking the consent, which is
// the reconnect — when the grant stops working.
func TestGitLabReportingStates(t *testing.T) {
	f, sess := newGLConnectFixture(t)
	const (
		path       = "/api/ui/workspace-settings/gitlab/grp/sub"
		connectURL = "/workspace-connect/gitlab/grp/sub"
	)

	got := decodeJSON[workspaceSettingsDTO](t, get(f.fixture, path, sess))
	if !got.Reporting.Available || got.Reporting.State != "off" || got.Reporting.ConnectURL != connectURL {
		t.Errorf("unconnected reporting = %+v, want the consent link", got.Reporting)
	}

	f.grant(t, "covbot", "rt-0", false)
	got = decodeJSON[workspaceSettingsDTO](t, get(f.fixture, path, sess))
	if got.Reporting.State != "on" || got.Reporting.Account != "covbot" {
		t.Errorf("connected reporting = %+v, want it posting as covbot", got.Reporting)
	}

	f.grant(t, "covbot", "rt-0", true)
	got = decodeJSON[workspaceSettingsDTO](t, get(f.fixture, path, sess))
	if got.Reporting.State != "broken" || got.Reporting.ConnectURL != connectURL {
		t.Errorf("broken reporting = %+v, want the reconnect link", got.Reporting)
	}
}
