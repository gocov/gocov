package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/hosted"
	"github.com/gocov/gocov/internal/store"
)

func TestReportsPostedMsg(t *testing.T) {
	// The message names the identity a build status will appear as, and
	// says nothing at all until that identity actually exists.
	for _, tc := range []struct {
		name string
		ws   *store.Workspace
		want string
	}{
		{"github app installed", &store.Workspace{Forge: "github", GitHubInstallationID: 42}, "gocov[bot]"},
		{"github not installed", &store.Workspace{Forge: "github"}, ""},
		{"bitbucket granted", &store.Workspace{Forge: "bitbucket", Grant: store.Grant{Account: "acme"}}, "@acme"},
		{"bitbucket no grant", &store.Workspace{Forge: "bitbucket"}, ""},
		{"gitlab granted", &store.Workspace{Forge: "gitlab", Grant: store.Grant{Account: "acme"}}, "@acme"},
		{"gitlab no grant", &store.Workspace{Forge: "gitlab"}, ""},
		{"unknown forge", &store.Workspace{Forge: "gitea", GitHubInstallationID: 42}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := reportsPostedMsg(tc.ws)
			if tc.want == "" {
				if got != "" {
					t.Errorf("reportsPostedMsg = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("reportsPostedMsg = %q, want it to name %q", got, tc.want)
			}
		})
	}
}

func TestAPIWorkspaceSetup(t *testing.T) {
	f, sess := newWorkspaceFixture(t, true) // an owner of acme, one repo
	const path = "/api/ui/workspace-setup/bitbucket/acme"

	rec := get(f, path, sess)
	wantStatus(t, rec, "GET setup", http.StatusOK)
	got := decodeJSON[setupInfoDTO](t, rec)
	if got.Workspace.Forge != "bitbucket" || got.Workspace.Prefix != "acme" {
		t.Errorf("workspace = %+v", got.Workspace)
	}
	if !got.Owner || got.TokenMasked == nil || strings.Contains(*got.TokenMasked, "ws-secret") {
		t.Errorf("owner = %v, token_masked = %v; want the masked form and never the token",
			got.Owner, got.TokenMasked)
	}
	// A self-hosted base URL is what CI needs spelled out, and it is also
	// the OIDC audience.
	if got.BaseURL != "https://gocov.example" || got.ServerImplicit {
		t.Errorf("base url = %q, server implicit = %v", got.BaseURL, got.ServerImplicit)
	}
	if got.CLIVersion != hosted.PinnedCLIVersion {
		t.Errorf("cli version = %q, want the pinned %q", got.CLIVersion, hosted.PinnedCLIVersion)
	}
	// Nothing is connected, so the snippet needs the token and the card
	// offers no reconnect.
	if got.Tokenless || got.ConnectionBroken || got.Reporting.State != "off" {
		t.Errorf("tokenless = %v, broken = %v, reporting = %+v", got.Tokenless, got.ConnectionBroken, got.Reporting)
	}
	if got.Status.RepoCount != 1 || got.Status.FirstReport != nil || got.Status.ReportsPosted != "" {
		t.Errorf("status before any report = %+v", got.Status)
	}
	// The poll answers with exactly the embedded status.
	if st := decodeJSON[setupStatusDTO](t, get(f, strings.Replace(path, "/workspace-setup/", "/workspace-setup-status/", 1), sess)); !reflect.DeepEqual(st, got.Status) {
		t.Errorf("poll = %+v, want the embedded status %+v", st, got.Status)
	}

	// Connect the workspace and land the first report: uploads can go
	// tokenless, and the poll flips to the payoff.
	ctx := t.Context()
	ws, err := f.store.WorkspaceByPrefix(ctx, "bitbucket", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetWorkspaceGrant(ctx, ws.ID, "bitbucket", store.Grant{Account: "acme-ci", RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}
	const sha = "0123456789abcdef0123456789abcdef01234567"
	if err := f.store.UpsertCommitReport(ctx, &store.CommitReport{
		RepoID: f.repo.ID, CommitSHA: sha, Branch: "main",
		TotalPct: 87.5, CoveredStmts: 35, TotalStmts: 40, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	st := decodeJSON[setupStatusDTO](t, get(f, strings.Replace(path, "/workspace-setup/", "/workspace-setup-status/", 1), sess))
	if st.FirstReport == nil {
		t.Fatalf("first report missing after an upload landed: %+v", st)
	}
	// The full SHA rides across; shortening is the client's business.
	want := setupFirstReportDTO{
		Repo:         repoRefDTO{Forge: "bitbucket", Slug: "acme/widgets"},
		Branch:       "main",
		SHA:          sha,
		Coverage:     87.5,
		CoveredStmts: 35,
		TotalStmts:   40,
	}
	if *st.FirstReport != want {
		t.Errorf("first report = %+v, want %+v", *st.FirstReport, want)
	}
	if !strings.Contains(st.ReportsPosted, "@acme-ci") {
		t.Errorf("reports posted = %q, want it to name the connected account", st.ReportsPosted)
	}
	got = decodeJSON[setupInfoDTO](t, get(f, path, sess))
	if !got.Tokenless || got.Status.FirstReport == nil || got.Reporting.State != "on" {
		t.Errorf("setup after connecting = tokenless %v, status %+v, reporting %+v",
			got.Tokenless, got.Status, got.Reporting)
	}
}

func TestAPIWorkspaceSetupAccess(t *testing.T) {
	// A member reads the setup screen — they need the snippet too — but
	// the token is not theirs, not even masked.
	f, sess := newMemberFixture(t, true)
	const path = "/api/ui/workspace-setup/bitbucket/acme"
	rec := get(f, path, sess)
	wantStatus(t, rec, "member GET setup", http.StatusOK)
	if got := decodeJSON[setupInfoDTO](t, rec); got.Owner || got.TokenMasked != nil {
		t.Errorf("member read owner = %v, token_masked = %v; want false/null", got.Owner, got.TokenMasked)
	}

	// A workspace the viewer is no member of must not even be confirmed.
	if err := f.store.CreateWorkspace(t.Context(),
		&store.Workspace{Forge: "bitbucket", Prefix: "beta", Token: "beta-tok", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"/api/ui/workspace-setup/bitbucket/beta",
		"/api/ui/workspace-setup-status/bitbucket/beta",
	} {
		wantStatus(t, get(f, p, sess), "non-member "+p, http.StatusNotFound)
	}
	// Signed out, the app is told to sign in rather than redirected.
	wantStatus(t, get(f, path), "signed-out setup", http.StatusUnauthorized)
	wantStatus(t, get(f, strings.Replace(path, "/workspace-setup/", "/workspace-setup-status/", 1)), "signed-out status", http.StatusUnauthorized)
}

// The setup screen shows the newest report among the workspace's repos,
// whichever repo it landed in.
func TestLatestReportIsTheNewest(t *testing.T) {
	f := newFixture(t, nil)
	ctx := t.Context()
	other := &store.Repo{Forge: "bitbucket", Slug: "acme/zeta", Token: "tok-z", DefaultBranch: "main"}
	if err := f.store.CreateRepo(ctx, other); err != nil {
		t.Fatal(err)
	}
	for _, cr := range []*store.CommitReport{
		{RepoID: other.ID, CommitSHA: "z1", Branch: "main", PartCount: 1},
		{RepoID: f.repo.ID, CommitSHA: "w1", Branch: "main", PartCount: 1},
		{RepoID: other.ID, CommitSHA: "z2", Branch: "main", PartCount: 1},
	} {
		if err := f.store.UpsertCommitReport(ctx, cr); err != nil {
			t.Fatal(err)
		}
	}
	repos, err := f.store.ListWorkspaceRepos(ctx, "bitbucket", "acme")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	repo, rep := f.srv.latestReport(r, repos)
	if repo == nil || repo.Slug != "acme/zeta" || rep.CommitSHA != "z2" {
		t.Errorf("latestReport = %v / %v, want acme/zeta's z2", repo, rep)
	}
	if repo, rep := f.srv.latestReport(r, nil); repo != nil || rep != nil {
		t.Errorf("no repos: %v / %v, want nils", repo, rep)
	}
}
