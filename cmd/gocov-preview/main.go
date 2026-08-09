// Command gocov-preview is a throwaway dev harness: it serves the web UI
// from an in-memory store seeded with a synthetic upload history, for
// eyeballing UI changes without Postgres. Not part of the product.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/bykclk/gocov/internal/auth"
	blobmem "github.com/bykclk/gocov/internal/blobstore/memory"
	"github.com/bykclk/gocov/internal/forge"
	"github.com/bykclk/gocov/internal/forge/bitbucket"
	forgefake "github.com/bykclk/gocov/internal/forge/fake"
	"github.com/bykclk/gocov/internal/profile"
	"github.com/bykclk/gocov/internal/server"
	"github.com/bykclk/gocov/internal/store"
	storemem "github.com/bykclk/gocov/internal/store/memory"
)

// devAuth is a sign-in provider that "authorizes" by bouncing straight
// back to the local callback, so the login, registration and workspace
// settings pages are previewable without a real OAuth consumer. Enable
// with GOCOV_PREVIEW_AUTH=1 (hosted mode; sign-in lands a member of the
// seeded acme workspace with the unregistered "personal" also on offer).
// The "github" instance signs in a member of the gh-* workspaces, which
// are seeded in the three GitHub App connection states.
type devAuth struct {
	forge      string
	workspaces []string
}

func (a devAuth) Name() string { return a.forge }
func (a devAuth) AuthorizeURL(state, redirectURI string) string {
	return redirectURI + "?state=" + url.QueryEscape(state) + "&code=dev"
}
func (a devAuth) Identity(context.Context, string, string) (*auth.Identity, error) {
	return &auth.Identity{
		ForgeUUID: "{dev-" + a.forge + "}", DisplayName: "Dev User", Email: "dev@example.com",
		Workspaces: a.workspaces,
	}, nil
}

// devGitHubApp stubs server.GitHubApp so the settings/setup pages render
// the App cards; the connect flow itself needs no live GitHub either.
type devGitHubApp struct{ fg forge.Forge }

func (d devGitHubApp) ForgeClient(context.Context, int64) (forge.Forge, error) { return d.fg, nil }
func (devGitHubApp) InstallationAccount(context.Context, int64) (string, error) {
	return "gh-new", nil
}
func (devGitHubApp) InstallURL(context.Context) (string, error) {
	return "https://github.com/apps/gocov/installations/new", nil
}

// devBBConnect stubs server.BitbucketConnect: the consent bounce goes
// straight back to the local callback, so the whole connect loop is
// previewable without Bitbucket.
type devBBConnect struct{ fg forge.Forge }

func (devBBConnect) AuthorizeURL(state, redirectURI string) string {
	return redirectURI + "?state=" + url.QueryEscape(state) + "&code=dev"
}
func (devBBConnect) Exchange(context.Context, string, string) (*bitbucket.Grant, error) {
	return &bitbucket.Grant{Account: "gocov-bot", AccessToken: "at", RefreshToken: "rt", TTL: 2 * time.Hour}, nil
}
func (devBBConnect) Refresh(context.Context, string) (*bitbucket.Grant, error) {
	return &bitbucket.Grant{AccessToken: "at", RefreshToken: "rt", TTL: 2 * time.Hour}, nil
}
func (d devBBConnect) ForgeClient(string) forge.Forge { return d.fg }

func main() {
	ctx := context.Background()
	st := storemem.New()
	repo := &store.Repo{
		Forge: "bitbucket", Slug: "acme/widgets", Token: "tok",
		DefaultBranch: "main", Gate: store.Gate{MinCoverage: pctPtr(70)},
	}
	if err := st.CreateRepo(ctx, repo); err != nil {
		log.Fatal(err)
	}
	if err := st.CreateWorkspace(ctx, &store.Workspace{
		Forge: "bitbucket", Prefix: "acme", Token: "ws-preview-token", DefaultBranch: "main",
	}); err != nil {
		log.Fatal(err)
	}

	// ~45 uploads drifting between ~68% and ~85%, a few gate failures,
	// a couple of PR uploads that must not appear in the trend.
	rnd := rand.New(rand.NewSource(42))
	base := time.Now().Add(-45 * 24 * time.Hour)
	pct := 74.0
	for i := 0; i < 45; i++ {
		pct += rnd.Float64()*4 - 2 + 0.1*math.Sin(float64(i)/4)
		pct = math.Max(66, math.Min(88, pct))
		u := &store.Upload{
			RepoID:    repo.ID,
			CommitSHA: fmt.Sprintf("%040x", i),
			Branch:    "main",
			Format:    "go",
			TotalPct:  pct, CoveredStmts: int64(pct * 10), TotalStmts: 1000,
			GateFailed: pct < 70,
			CreatedAt:  base.Add(time.Duration(i) * 24 * time.Hour),
		}
		if i%15 == 7 {
			u.PRID = "9"
			u.TotalPct = 20 // would be an obvious outlier if it leaked in
		}
		if err := st.CreateUpload(ctx, u, nil); err != nil {
			log.Fatal(err)
		}
	}

	// GitHub and Bitbucket workspaces in the connection states One-Click
	// Connect adds, for the settings/setup page cards. The default acme
	// workspace stays unconnected — that is the Connect-button state.
	for _, ws := range []*store.Workspace{
		{Forge: "github", Prefix: "gh-new", Token: "gh-new-token", DefaultBranch: "main"},
		{Forge: "github", Prefix: "gh-connected", Token: "gh-conn-token", DefaultBranch: "main",
			GitHubInstallationID: 4242},
		{Forge: "github", Prefix: "gh-broken", Token: "gh-broken-token", DefaultBranch: "main",
			GitHubInstallationID: 4243, GitHubAppBroken: true},
		{Forge: "bitbucket", Prefix: "bb-connected", Token: "bb-conn-token", DefaultBranch: "main",
			BitbucketGrantAccount: "gocov-bot", BitbucketRefreshToken: "rt"},
		{Forge: "bitbucket", Prefix: "bb-broken", Token: "bb-broken-token", DefaultBranch: "main",
			BitbucketGrantAccount: "gocov-bot", BitbucketRefreshToken: "rt", BitbucketGrantBroken: true},
	} {
		if err := st.CreateWorkspace(ctx, ws); err != nil {
			log.Fatal(err)
		}
	}

	var auths []auth.Provider
	hosted := false
	if os.Getenv("GOCOV_PREVIEW_AUTH") == "1" {
		auths = []auth.Provider{
			devAuth{forge: "bitbucket", workspaces: []string{"acme", "personal", "bb-connected", "bb-broken"}},
			devAuth{forge: "github", workspaces: []string{"gh-new", "gh-connected", "gh-broken"}},
		}
		hosted = true
		log.Println("preview auth on: sign-in via bitbucket lands in acme, via github in the gh-* workspaces")
	}
	srv := server.New(server.Config{
		Store: st, Blobs: blobmem.New(),
		Parsers: map[string]profile.Parser{"go": profile.GoParser{}},
		Forges: map[string]forge.Factory{
			"bitbucket": forgefake.New().Factory(),
			"github":    forgefake.New().Factory(),
		},
		BaseURL:          "http://localhost:8099",
		Auths:            auths,
		Hosted:           hosted,
		GitHubApp:        devGitHubApp{fg: forgefake.New()},
		BitbucketConnect: devBBConnect{fg: forgefake.New()},
	})
	log.Println("preview on :8099")
	log.Fatal(http.ListenAndServe(":8099", srv))
}

func pctPtr(v float64) *float64 { return &v }
