// Anonymous read-only report pages for public repos, end to end: which
// pages open without a session, which keep the login wall, and the
// switches (per repo and per instance) that close them again. The access
// decision itself lives in scope.go; these tests drive it through the
// routed pages like a signed-out browser would.

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gocov/gocov/internal/auth"
	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

// newPublicFixture builds a sign-in-enabled server over the repo
// acme/widgets with the given forge-reported visibility. instanceOn is
// the GOCOV_PUBLIC_REPORTS switch. A workspace row for acme exists, so a
// member signing in through the fake provider lands with a membership.
func newPublicFixture(t *testing.T, visibility string, instanceOn bool) *fixture {
	t.Helper()
	ctx := context.Background()
	st := storemem.New()
	repo := &store.Repo{
		Forge: "bitbucket", Slug: "acme/widgets", Token: "secret-token",
		DefaultBranch: "main", Visibility: visibility,
	}
	if err := st.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	ws := &store.Workspace{Forge: "bitbucket", Prefix: "acme", Token: "ws-secret", DefaultBranch: "main"}
	if err := st.CreateWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	blobs := blobmem.New()
	srv := New(Config{
		Store:         st,
		Blobs:         blobs,
		Parsers:       map[string]profile.Parser{"go": profile.GoParser{}},
		BaseURL:       "https://gocov.example",
		Auths:         []auth.Provider{&fakeProvider{identity: memberIdentity()}},
		PublicReports: instanceOn,
	})
	return &fixture{srv: srv, store: st, blobs: blobs, repo: repo}
}

// seedUpload stores one upload with per-file rows and its raw profile
// blob, the way the upload API would have left them.
func seedUpload(t *testing.T, f *fixture) *store.Upload {
	t.Helper()
	ctx := context.Background()
	u := &store.Upload{
		RepoID: f.repo.ID, CommitSHA: "abc1234def", Branch: "main", Format: "go",
		TotalPct: 80, CoveredStmts: 8, TotalStmts: 10,
		RawBlobKey: "profiles/1/raw", Part: "default",
	}
	files := []*store.UploadFile{{
		Path: "a.go", Pct: 80, CoveredStmts: 8, TotalStmts: 10,
		Blocks: []profile.Block{
			{StartLine: 1, EndLine: 2, NumStmts: 8, Count: 1},
			{StartLine: 3, EndLine: 4, NumStmts: 2, Count: 0},
		},
	}}
	if err := f.store.CreateUpload(ctx, u, files); err != nil {
		t.Fatal(err)
	}
	if err := f.blobs.Put(ctx, u.RawBlobKey, []byte(testProfile)); err != nil {
		t.Fatal(err)
	}
	return u
}

func wantLoginRedirect(t *testing.T, rec *httptest.ResponseRecorder, path string) {
	t.Helper()
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Errorf("GET %s anonymous: %d -> %q, want the login redirect", path, rec.Code, rec.Header().Get("Location"))
	}
}

func TestPublicRepoReportPagesOpenAnonymously(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	u := seedUpload(t, f)

	repoPage := get(f, "/repos/acme/widgets")
	if repoPage.Code != http.StatusOK {
		t.Fatalf("repo page anonymous: status = %d", repoPage.Code)
	}
	body := repoPage.Body.String()
	if !strings.Contains(body, "acme/widgets") {
		t.Error("repo page misses the slug")
	}
	// Read-only: no settings link, no member chrome — and the visitor CTA
	// band is there.
	if strings.Contains(body, "/repo-settings/") {
		t.Error("anonymous repo page shows the settings link")
	}
	if strings.Contains(body, "Sign out") {
		t.Error("anonymous repo page shows the signed-in chrome")
	}
	if !strings.Contains(body, "public-cta") {
		t.Error("anonymous repo page misses the CTA band")
	}
	// The anonymous render is cacheable — the no-store header belongs to
	// member sessions only.
	if cc := repoPage.Header().Get("Cache-Control"); cc == "no-store" {
		t.Errorf("anonymous public page sent Cache-Control: %q", cc)
	}

	uploadPage := get(f, "/uploads/1")
	if uploadPage.Code != http.StatusOK {
		t.Fatalf("upload page anonymous: status = %d", uploadPage.Code)
	}
	if !strings.Contains(uploadPage.Body.String(), "public-cta") {
		t.Error("anonymous upload page misses the CTA band")
	}

	src := get(f, "/uploads/1/files/a.go")
	if src.Code != http.StatusOK {
		t.Fatalf("source page anonymous: status = %d", src.Code)
	}

	// Crawlers probe with HEAD; the mux serves it through the GET route,
	// so the sessionless pass-through must admit it too.
	headReq := httptest.NewRequest(http.MethodHead, "/repos/acme/widgets", nil)
	headRec := httptest.NewRecorder()
	f.srv.ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusOK {
		t.Errorf("HEAD repo page anonymous: status = %d", headRec.Code)
	}

	prof := get(f, "/uploads/1/profile")
	if prof.Code != http.StatusOK {
		t.Fatalf("raw profile anonymous: status = %d", prof.Code)
	}
	if prof.Body.String() != testProfile {
		t.Errorf("raw profile body = %q", prof.Body.String())
	}
	_ = u
}

func TestNonPublicRepoKeepsLoginWallForAnonymous(t *testing.T) {
	for _, visibility := range []string{"", store.VisibilityPrivate} {
		f := newPublicFixture(t, visibility, true)
		seedUpload(t, f)

		// Today's behavior exactly, and indistinguishable from a slug or
		// upload that does not exist — a signed-out probe learns nothing.
		for _, path := range []string{
			"/repos/acme/widgets",
			"/repos/no/such",
			"/uploads/1",
			"/uploads/1/profile",
			"/uploads/1/files/a.go",
			"/uploads/999",
			"/uploads/notanid",
		} {
			wantLoginRedirect(t, get(f, path), path)
		}
	}
}

func TestRepoSettingsSwitchClosesPublicPages(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	seedUpload(t, f)

	f.repo.PublicReportsDisabled = true
	if err := f.store.UpdateRepo(context.Background(), f.repo); err != nil {
		t.Fatal(err)
	}
	wantLoginRedirect(t, get(f, "/repos/acme/widgets"), "/repos/acme/widgets")
	wantLoginRedirect(t, get(f, "/uploads/1"), "/uploads/1")
}

func TestInstanceSwitchClosesPublicPages(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, false)
	seedUpload(t, f)

	wantLoginRedirect(t, get(f, "/repos/acme/widgets"), "/repos/acme/widgets")
	wantLoginRedirect(t, get(f, "/uploads/1"), "/uploads/1")
	wantLoginRedirect(t, get(f, "/uploads/1/profile"), "/uploads/1/profile")
}

func TestMemberViewOfPublicRepoIsUnchanged(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	seedUpload(t, f)
	sess := signIn(t, f, "/")

	rec := get(f, "/repos/acme/widgets", sess)
	if rec.Code != http.StatusOK {
		t.Fatalf("member repo page: status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "public-cta") {
		t.Error("signed-in member sees the visitor CTA band")
	}
	if !strings.Contains(body, "/repo-settings/acme/widgets") {
		t.Error("member repo page misses the settings link")
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("member page Cache-Control = %q, want no-store", cc)
	}
}

func TestPublicReportsToggleInRepoSettings(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	sess := signIn(t, f, "/")

	// The switch renders for a public repo.
	page := get(f, "/repo-settings/acme/widgets", sess)
	if page.Code != http.StatusOK {
		t.Fatalf("settings page: status = %d", page.Code)
	}
	if !strings.Contains(page.Body.String(), "Public reports") {
		t.Error("settings page misses the Public reports switch")
	}

	// Saving without the checkbox turns public pages off at once.
	rec := postForm(f, "/repo-settings/save/acme/widgets", url.Values{"default_branch": {"main"}}, sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: status = %d, body = %s", rec.Code, rec.Body)
	}
	repo, err := f.store.RepoBySlug(context.Background(), "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	if !repo.PublicReportsDisabled {
		t.Error("saving with the checkbox off did not disable public reports")
	}
	wantLoginRedirect(t, get(f, "/repos/acme/widgets"), "/repos/acme/widgets")

	// Saving with the checkbox on reopens them.
	rec = postForm(f, "/repo-settings/save/acme/widgets", url.Values{"default_branch": {"main"}, "public_reports": {"on"}}, sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("re-save: status = %d", rec.Code)
	}
	if rec := get(f, "/repos/acme/widgets"); rec.Code != http.StatusOK {
		t.Errorf("public page after reopening: status = %d", rec.Code)
	}
}

func TestPrivateRepoSettingsHideTheSwitchAndKeepTheValue(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPrivate, true)
	sess := signIn(t, f, "/")

	page := get(f, "/repo-settings/acme/widgets", sess)
	if strings.Contains(page.Body.String(), "Public reports") {
		t.Error("private repo settings render the Public reports switch")
	}
	// A save without the (absent) checkbox must not flip the stored value.
	if rec := postForm(f, "/repo-settings/save/acme/widgets", url.Values{"default_branch": {"main"}}, sess); rec.Code != http.StatusSeeOther {
		t.Fatalf("save: status = %d", rec.Code)
	}
	repo, err := f.store.RepoBySlug(context.Background(), "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	if repo.PublicReportsDisabled {
		t.Error("saving a private repo's settings flipped PublicReportsDisabled")
	}
}

// TestUploadRefreshesVisibility drives the whole loop: a repo whose forge
// flips it public becomes anonymously viewable by the next upload.
func TestUploadRefreshesVisibility(t *testing.T) {
	f := newFixture(t, map[string]string{})
	f.forge.Visibility = store.VisibilityPublic

	rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "branch": "main"}, testProfile)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: status = %d, body = %s", rec.Code, rec.Body)
	}
	repo, err := f.store.RepoBySlug(context.Background(), "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	if repo.Visibility != store.VisibilityPublic {
		t.Errorf("visibility after upload = %q, want %q", repo.Visibility, store.VisibilityPublic)
	}

	// Flipped back private on the forge: corrected on the next upload.
	f.forge.Visibility = store.VisibilityPrivate
	if rec := doUpload(t, f, "secret-token", map[string]string{"commit": "c2", "branch": "main"}, testProfile); rec.Code != http.StatusCreated {
		t.Fatalf("second upload: status = %d", rec.Code)
	}
	repo, err = f.store.RepoBySlug(context.Background(), "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	if repo.Visibility != store.VisibilityPrivate {
		t.Errorf("visibility after flip = %q, want %q", repo.Visibility, store.VisibilityPrivate)
	}
}
