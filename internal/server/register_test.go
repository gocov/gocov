package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/auth"
	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// newHostedFixture builds a hosted-mode server with an empty
// store: no tracked workspaces, like a fresh SaaS instance.
func newHostedFixture(t *testing.T, provider auth.Provider) *fixture {
	t.Helper()
	st := storemem.New()
	srv := New(Config{
		Store:   st,
		Blobs:   blobmem.New(),
		BaseURL: "https://gocov.example",
		Auths:   []auth.Provider{provider},
		Hosted:  true,
	})
	return &fixture{srv: srv, store: st}
}

// hostedSignIn drives the OAuth flow and asserts the callback lands on
// wantNext, returning the session cookie.
func hostedSignIn(t *testing.T, f *fixture, next, wantNext string) *http.Cookie {
	t.Helper()
	start := get(f, "/oauth/bitbucket/start?next="+url.QueryEscape(next))
	if start.Code != http.StatusFound {
		t.Fatalf("start: status = %d", start.Code)
	}
	stateCk := cookieNamed(t, start, stateCookie)
	state, _, _ := strings.Cut(stateCk.Value, "|")
	cb := get(f, "/oauth/bitbucket/callback?code=thecode&state="+url.QueryEscape(state), stateCk)
	if cb.Code != http.StatusFound {
		t.Fatalf("callback: status = %d", cb.Code)
	}
	if loc := cb.Header().Get("Location"); loc != wantNext {
		t.Fatalf("callback redirected to %q, want %q", loc, wantNext)
	}
	return cookieNamed(t, cb, sessionCookie)
}

// postRegister claims a workspace the way the app does.
func postRegister(t *testing.T, f *fixture, prefix string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return postJSON(t, f, "/api/ui/onboarding/register", registerInput{Prefix: prefix}, cookies...)
}

func TestHostedAdmitsNonMemberAndRoutesToRegistration(t *testing.T) {
	// This identity would be denied on a private instance (no tracked
	// workspace matches); hosted mode signs it in and lands on /register.
	f := newHostedFixture(t, &fakeProvider{identity: memberIdentity()})

	sess := hostedSignIn(t, f, "/", "/onboarding")

	// The user exists with the login-time workspace snapshot.
	users, err := f.store.ListUsers(t.Context())
	if err != nil || len(users) != 1 {
		t.Fatalf("users = %v, %v", users, err)
	}
	if !reflect.DeepEqual(users[0].ForgeWorkspaces, []string{"acme", "personal"}) {
		t.Errorf("stored forge workspaces = %v", users[0].ForgeWorkspaces)
	}

	// The dashboard itself is the shell like every other page; the app is
	// told to route a membership-less hosted user to onboarding.
	if rec := get(f, "/", sess); rec.Code != http.StatusOK {
		t.Errorf("index: status = %d, want the shell", rec.Code)
	}
	if got := decodeJSON[dashboardDTO](t, get(f, "/api/ui/dashboard", sess)); !got.NeedsOnboarding {
		t.Errorf("dashboard = %+v, want needs_onboarding for a hosted user with no workspace", got)
	}
}

func TestHostedUserWithMembershipLandsOnIndex(t *testing.T) {
	f := newHostedFixture(t, &fakeProvider{identity: memberIdentity()})
	if err := f.store.CreateWorkspace(t.Context(),
		&store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "ws-tok", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}

	sess := hostedSignIn(t, f, "/", "/")
	if rec := get(f, "/", sess); rec.Code != http.StatusOK {
		t.Errorf("index for member: status = %d", rec.Code)
	}
}

func TestRegistrationWorksInPrivateMode(t *testing.T) {
	// A signed-in private-mode user can register a workspace their forge
	// vouches for — the bootstrap path now that the admin CLI is gone. The
	// signed-in identity is a member of "acme" and "personal" (memberIdentity).
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)
	sess := signIn(t, f, "/")

	// Claiming a forge-vouched workspace creates it.
	rec := postRegister(t, f, "personal", sess)
	wantStatus(t, rec, "register", http.StatusOK)
	if res := decodeJSON[registerResultDTO](t, rec); !res.Created || res.Prefix != "personal" {
		t.Errorf("register result = %+v, want the created workspace", res)
	}
	if _, err := f.store.WorkspaceByPrefix(t.Context(), "bitbucket", "personal"); err != nil {
		t.Fatalf("workspace not registered: %v", err)
	}
	// The forge-list check still gates: a workspace the identity does not list.
	wantStatus(t, postRegister(t, f, "notmine", sess), "register an unvouched workspace", http.StatusNotFound)
}

// TestHostedReauthHonorsPendingInstall locks the callback change behind the
// GitHub install self-heal: a re-auth aimed at a pending install must reach
// /github/setup even for a zero-membership user, instead of being forced to
// /onboarding, so it can connect once the org snapshot refreshes.
func TestHostedReauthHonorsPendingInstall(t *testing.T) {
	f := newHostedFixture(t, &fakeProvider{identity: memberIdentity()})
	next := "/github/setup?installation_id=5"
	hostedSignIn(t, f, next, next) // asserts the callback honours next verbatim
}

func TestRegisterCreatesWorkspaceAndSeatsTheFounder(t *testing.T) {
	f := newHostedFixture(t, &fakeProvider{identity: memberIdentity()})
	sess := hostedSignIn(t, f, "/", "/onboarding")

	wantStatus(t, postRegister(t, f, "personal", sess), "register", http.StatusOK)

	ctx := t.Context()
	ws, err := f.store.WorkspaceByPrefix(ctx, "bitbucket", "personal")
	if err != nil {
		t.Fatalf("workspace not created: %v", err)
	}
	if ws.Forge != "bitbucket" || ws.Token == "" || ws.DefaultBranch != "main" {
		t.Errorf("workspace = %+v", ws)
	}
	// The setup screen hands the founder the token to paste into CI.
	reveal := postJSON(t, f, "/api/ui/workspace-settings/reveal-token/bitbucket/personal", nil, sess)
	wantStatus(t, reveal, "reveal token", http.StatusOK)
	if got := decodeJSON[tokenRevealDTO](t, reveal).Token; got != ws.Token {
		t.Errorf("revealed token = %q, want the workspace's", got)
	}

	// Registration made the user a member atomically, as the owner...
	users, _ := f.store.ListUsers(ctx)
	wss, err := f.store.ListWorkspacesForUser(ctx, users[0].ID)
	if err != nil || len(wss) != 1 || wss[0].Prefix != "personal" {
		t.Fatalf("memberships after registration = %v, %v", wss, err)
	}
	if got := membershipRoles(t, f, users[0].ID); !reflect.DeepEqual(got, map[string]store.Role{"personal": store.RoleOwner}) {
		t.Errorf("roles after registration = %v, want the founder as owner", got)
	}
	// ...so the dashboard has something to show instead of asking for onboarding.
	if got := decodeJSON[dashboardDTO](t, get(f, "/api/ui/dashboard", sess)); got.NeedsOnboarding {
		t.Error("dashboard still asks for onboarding after the claim")
	}
}

func TestRegisterSameNameOnAnotherForgeIsItsOwnWorkspace(t *testing.T) {
	// Names are scoped per forge: the GitHub org "acme" and the Bitbucket
	// workspace "acme" are two tenants, so the Bitbucket user creates
	// theirs beside the GitHub one rather than being turned away.
	f := newHostedFixture(t, &fakeProvider{identity: memberIdentity()})
	gh := &store.Workspace{Forge: "github", Prefix: "acme", Token: "gh-tok", DefaultBranch: "main"}
	if err := f.store.CreateWorkspace(t.Context(), gh); err != nil {
		t.Fatal(err)
	}
	sess := hostedSignIn(t, f, "/", "/onboarding")

	wantStatus(t, postRegister(t, f, "acme", sess), "claim beside another forge's namesake", http.StatusOK)
	bb, err := f.store.WorkspaceByPrefix(t.Context(), "bitbucket", "acme")
	if err != nil {
		t.Fatalf("bitbucket acme not registered: %v", err)
	}
	if bb.ID == gh.ID || bb.Token == gh.Token {
		t.Error("the claim joined the GitHub workspace instead of creating a Bitbucket one")
	}
}

func TestRegisterAlreadyRegisteredJoins(t *testing.T) {
	// A colleague registered "acme" after this user's login sync; claiming
	// it is a non-event: membership is granted, no new workspace.
	// Joining is open to plain members — only creating takes an owner.
	f := newHostedFixture(t, &fakeProvider{identity: plainMemberIdentity()})
	sess := hostedSignIn(t, f, "/", "/onboarding")
	ctx := t.Context()
	if err := f.store.CreateWorkspace(ctx,
		&store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "ws-tok", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}

	rec := postRegister(t, f, "acme", sess)
	wantStatus(t, rec, "join", http.StatusOK)
	if res := decodeJSON[registerResultDTO](t, rec); res.Created {
		t.Errorf("join result = %+v, want created=false", res)
	}
	users, _ := f.store.ListUsers(ctx)
	wss, err := f.store.ListWorkspacesForUser(ctx, users[0].ID)
	if err != nil || len(wss) != 1 || wss[0].Prefix != "acme" {
		t.Errorf("memberships after join = %v, %v", wss, err)
	}
	// The forge snapshot says plain member, so that is the seat granted.
	if got := membershipRoles(t, f, users[0].ID); !reflect.DeepEqual(got, map[string]store.Role{"acme": store.RoleMember}) {
		t.Errorf("roles after join = %v, want member", got)
	}
	// Idempotent: joining again keeps the single membership.
	wantStatus(t, postRegister(t, f, "acme", sess), "second join", http.StatusOK)
	if wss, _ := f.store.ListWorkspacesForUser(ctx, users[0].ID); len(wss) != 1 {
		t.Errorf("second join changed memberships: %v", wss)
	}
}

func TestJoinTakesTheForgeRole(t *testing.T) {
	// Same join, but the forge says the account administers acme: the
	// seat granted is an owner's, exactly what the next login sync gives.
	identity := memberIdentity()
	identity.OwnedWorkspaces = []string{"acme"}
	f := newHostedFixture(t, &fakeProvider{identity: identity})
	sess := hostedSignIn(t, f, "/", "/onboarding")
	if err := f.store.CreateWorkspace(t.Context(),
		&store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "ws-tok", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, postRegister(t, f, "acme", sess), "join", http.StatusOK)
	users, _ := f.store.ListUsers(t.Context())
	if got := membershipRoles(t, f, users[0].ID); !reflect.DeepEqual(got, map[string]store.Role{"acme": store.RoleOwner}) {
		t.Errorf("roles after join = %v, want owner", got)
	}
}

func TestReLoginRefreshesForgeWorkspaces(t *testing.T) {
	provider := &fakeProvider{identity: memberIdentity()}
	f := newHostedFixture(t, provider)
	hostedSignIn(t, f, "/", "/onboarding")

	provider.identity.Workspaces = []string{"acme", "personal", "newco"}
	provider.identity.OwnedWorkspaces = []string{"newco"}
	hostedSignIn(t, f, "/", "/onboarding")

	users, _ := f.store.ListUsers(t.Context())
	if len(users) != 1 || !reflect.DeepEqual(users[0].ForgeWorkspaces, []string{"acme", "personal", "newco"}) {
		t.Errorf("stored list not refreshed: %+v", users)
	}
	if len(users) != 1 || !reflect.DeepEqual(users[0].ForgeOwnedWorkspaces, []string{"newco"}) {
		t.Errorf("stored owned list not refreshed: %+v", users)
	}
}

func TestLoginSyncsRolesFromTheForge(t *testing.T) {
	// Roles mirror the forge like membership does: an admin there is an
	// owner here, and the next sign-in re-syncs both directions —
	// promotion and demotion — without touching the membership itself.
	identity := memberIdentity() // acme and personal, owner of neither
	provider := &fakeProvider{identity: identity}
	f := newHostedFixture(t, provider)
	ctx := t.Context()
	for _, prefix := range []string{"acme", "personal"} {
		if err := f.store.CreateWorkspace(ctx,
			&store.Workspace{Forge: "bitbucket", Prefix: prefix, Token: prefix + "-tok", DefaultBranch: "main"}); err != nil {
			t.Fatal(err)
		}
	}

	identity.OwnedWorkspaces = []string{"personal"}
	hostedSignIn(t, f, "/", "/")
	users, _ := f.store.ListUsers(ctx)
	want := map[string]store.Role{"acme": store.RoleMember, "personal": store.RoleOwner}
	if got := membershipRoles(t, f, users[0].ID); !reflect.DeepEqual(got, want) {
		t.Errorf("roles after first sign-in = %v, want %v", got, want)
	}

	// Swapped on the forge: promoted in acme, demoted in personal.
	identity.OwnedWorkspaces = []string{"acme"}
	hostedSignIn(t, f, "/", "/")
	want = map[string]store.Role{"acme": store.RoleOwner, "personal": store.RoleMember}
	if got := membershipRoles(t, f, users[0].ID); !reflect.DeepEqual(got, want) {
		t.Errorf("roles after re-sync = %v, want %v", got, want)
	}
}

// membershipRoles maps the user's memberships to their roles by workspace
// prefix.
func membershipRoles(t *testing.T, f *fixture, userID int64) map[string]store.Role {
	t.Helper()
	ctx := t.Context()
	ms, err := f.store.ListMembershipsForUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	wss, err := f.store.ListWorkspaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prefixes := make(map[int64]string, len(wss))
	for _, ws := range wss {
		prefixes[ws.ID] = ws.Prefix
	}
	out := make(map[string]store.Role, len(ms))
	for _, m := range ms {
		out[prefixes[m.WorkspaceID]] = m.Role
	}
	return out
}

func TestAPIOnboardingAndRegister(t *testing.T) {
	f := newHostedFixture(t, &fakeProvider{identity: memberIdentity()})
	sess := hostedSignIn(t, f, "/", "/onboarding")

	rec := get(f, "/api/ui/onboarding", sess)
	wantStatus(t, rec, "GET onboarding", http.StatusOK)
	got := decodeJSON[onboardingDTO](t, rec)
	if got.Forge != "bitbucket" || got.Account != "Jane Dev" {
		t.Errorf("onboarding = %+v, want the signed-in Bitbucket account", got)
	}
	if got.Mode != "pick" || got.InstallURL != "" || got.MembershipCount != 2 {
		t.Errorf("mode = %q, install url = %q, memberships = %d; want the picker",
			got.Mode, got.InstallURL, got.MembershipCount)
	}
	// Nothing is registered yet, so both memberships are free to claim.
	want := []onboardingRowDTO{{Prefix: "acme", State: "available"}, {Prefix: "personal", State: "available"}}
	if !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows = %+v, want %+v", got.Rows, want)
	}

	// Claiming a free one creates it, and says so.
	rec = postJSON(t, f, "/api/ui/onboarding/register", registerInput{Prefix: "personal"}, sess)
	wantStatus(t, rec, "POST register", http.StatusOK)
	res := decodeJSON[registerResultDTO](t, rec)
	if res.Forge != "bitbucket" || res.Prefix != "personal" || !res.Created {
		t.Errorf("register result = %+v, want the created Bitbucket workspace", res)
	}
	ctx := t.Context()
	ws, err := f.store.WorkspaceByPrefix(ctx, "bitbucket", "personal")
	if err != nil || ws.Token == "" {
		t.Fatalf("workspace not registered: %+v, %v", ws, err)
	}
	// ...and the row it came from is the viewer's from now on.
	got = decodeJSON[onboardingDTO](t, get(f, "/api/ui/onboarding", sess))
	want = []onboardingRowDTO{{Prefix: "acme", State: "available"}, {Prefix: "personal", State: "member"}}
	if !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows after the claim = %+v, want %+v", got.Rows, want)
	}

	// One a colleague registered first is a join: no second workspace, and
	// membership granted now rather than at the next login sync.
	if err := f.store.CreateWorkspace(ctx,
		&store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "acme-tok", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	rec = postJSON(t, f, "/api/ui/onboarding/register", registerInput{Prefix: "acme"}, sess)
	wantStatus(t, rec, "POST register (join)", http.StatusOK)
	if res := decodeJSON[registerResultDTO](t, rec); res.Prefix != "acme" || res.Created {
		t.Errorf("join result = %+v, want created=false", res)
	}
	users, _ := f.store.ListUsers(ctx)
	wss, err := f.store.ListWorkspacesForUser(ctx, users[0].ID)
	if err != nil || len(wss) != 2 {
		t.Errorf("memberships after the join = %v, %v, want both workspaces", wss, err)
	}
}

func TestAPIRegisterRefusals(t *testing.T) {
	f := newHostedFixture(t, &fakeProvider{identity: memberIdentity()})
	sess := hostedSignIn(t, f, "/", "/onboarding")

	// A prefix the forge never vouched for does not exist as far as the app
	// is concerned — it only ever posts rows it was handed — while an empty
	// one is the picker failing validation.
	for _, tc := range []struct {
		prefix string
		want   int
	}{
		{"evilcorp", http.StatusNotFound},
		{"", http.StatusUnprocessableEntity},
		{"   ", http.StatusUnprocessableEntity},
	} {
		rec := postJSON(t, f, "/api/ui/onboarding/register", registerInput{Prefix: tc.prefix}, sess)
		wantStatus(t, rec, "POST register "+tc.prefix, tc.want)
	}
	if _, err := f.store.WorkspaceByPrefix(t.Context(), "bitbucket", "evilcorp"); err == nil {
		t.Error("a refused claim created a workspace")
	}

	// Creating a workspace mints its upload token, so it takes an owner on
	// the forge — the picker marks the row, and the POST is refused anyway.
	plain := newHostedFixture(t, &fakeProvider{identity: plainMemberIdentity()})
	plainSess := hostedSignIn(t, plain, "/", "/onboarding")
	if got := decodeJSON[onboardingDTO](t, get(plain, "/api/ui/onboarding", plainSess)); got.Rows[0].State != "unowned" {
		t.Errorf("rows for a plain member = %+v, want them unowned", got.Rows)
	}
	rec := postJSON(t, plain, "/api/ui/onboarding/register", registerInput{Prefix: "personal"}, plainSess)
	wantStatus(t, rec, "POST register as a plain member", http.StatusForbidden)
}

func TestAPIOnboardingInstallMode(t *testing.T) {
	// GitHub with the App configured creates the workspace on GitHub's own
	// install screen, so the app gets that URL instead of a picker.
	f, sess := newGitHubAppFixture(t, true, false)
	rec := get(f.fixture, "/api/ui/onboarding", sess)
	wantStatus(t, rec, "GET onboarding", http.StatusOK)
	got := decodeJSON[onboardingDTO](t, rec)
	if got.Mode != "install" || got.InstallURL != "https://github.com/apps/gocov/installations/new" {
		t.Errorf("onboarding = %+v, want the install prompt", got)
	}
	if len(got.Rows) != 0 || !strings.Contains(rec.Body.String(), `"rows":[]`) {
		t.Errorf("rows in install mode = %+v, want an empty array:\n%s", got.Rows, rec.Body)
	}
}

func TestAPIOnboardingNeedsAnIdentity(t *testing.T) {
	// Signed out, the middleware answers for the whole API; with no sign-in
	// provider at all there is no identity to register from, so both
	// endpoints are gone — in JSON, like every other UI API 404.
	f := newAuthFixture(t, &fakeProvider{identity: memberIdentity()}, nil)
	wantStatus(t, get(f, "/api/ui/onboarding"), "signed-out onboarding", http.StatusUnauthorized)
	wantStatus(t, postJSON(t, f, "/api/ui/onboarding/register", registerInput{Prefix: "acme"}),
		"signed-out register", http.StatusUnauthorized)

	open := newAuthFixture(t, nil, nil)
	rec := get(open, "/api/ui/onboarding")
	wantStatus(t, rec, "open-instance onboarding", http.StatusNotFound)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
	wantStatus(t, postJSON(t, open, "/api/ui/onboarding/register", registerInput{Prefix: "acme"}),
		"open-instance register", http.StatusNotFound)
}

func TestHostedLoginPageHasNoDenialOrWorkspaceHints(t *testing.T) {
	f := newHostedFixture(t, &fakeProvider{identity: memberIdentity()})
	if err := f.store.CreateWorkspace(t.Context(),
		&store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "ws-tok", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}

	// Even with the denial flag forced by URL, a hosted instance must
	// neither refuse nor disclose which workspaces it tracks.
	if rec := get(f, "/login?denied=1"); rec.Code != http.StatusOK {
		t.Errorf("hosted /login?denied=1: status = %d, want 200", rec.Code)
	}
	got := decodeJSON[loginInfoDTO](t, get(f, "/api/ui/login?denied=1"))
	if !got.Hosted {
		t.Error("hosted instance not reported as hosted")
	}
	if len(got.TrackedWorkspaces) != 0 {
		t.Errorf("hosted denial discloses tracked workspaces: %+v", got.TrackedWorkspaces)
	}
}
