package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/oidc"
	"github.com/gocov/gocov/internal/readapi"
	"github.com/gocov/gocov/internal/store"
)

// seedCommitReport stores a merged report the way the upload pipeline
// would have left one.
func seedCommitReport(t *testing.T, f *fixture, cr *store.CommitReport) {
	t.Helper()
	cr.RepoID = f.repo.ID
	if cr.CreatedAt.IsZero() {
		cr.CreatedAt = time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
		cr.UpdatedAt = cr.CreatedAt
	}
	if err := f.store.UpsertCommitReport(t.Context(), cr); err != nil {
		t.Fatal(err)
	}
}

// prReport is a two-part PR commit whose diff left lines untested in one
// file and none in another.
func prReport() *store.CommitReport {
	return &store.CommitReport{
		CommitSHA: "feed1234", Branch: "feature", PRID: "42",
		TotalPct: 80, CoveredStmts: 80, TotalStmts: 100,
		PartCount: 2, UploadID: 7,
		DiffCoverage: &diffcov.Result{
			Files: []diffcov.FileCoverage{
				{Path: "cmd/gocov/oidc.go", CoveredLines: 19, TotalLines: 23, UncoveredLines: []int{49, 50, 57, 58}},
				{Path: "cmd/gocov/main.go", CoveredLines: 5, TotalLines: 5},
			},
			CoveredLines: 24, TotalLines: 28,
			UnmatchedFiles: []string{"docs/cli.md"},
		},
	}
}

func TestReportAPICommit(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	seedCommitReport(t, f, prReport())

	rec := get(f, "/api/v1/reports/bitbucket/acme/widgets?commit=feed1234")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	got := decodeJSON[readapi.Report](t, rec)
	if got.Commit != "feed1234" || got.Branch != "feature" || got.PRID != "42" || got.Parts != 2 {
		t.Errorf("head = %+v", got)
	}
	if got.Coverage != 80 || got.CoveredStatements != 80 || got.TotalStatements != 100 {
		t.Errorf("totals = %v %d/%d", got.Coverage, got.CoveredStatements, got.TotalStatements)
	}
	// Two parts: the link is the merged page, as on the forge surfaces.
	if want := "https://gocov.example/uploads/7?parts=merged"; got.ReportURL != want {
		t.Errorf("report_url = %q, want %q", got.ReportURL, want)
	}
	if got.Diff == nil {
		t.Fatal("diff is null")
	}
	// Only the file with untested changed lines is listed, both spellings.
	want := []readapi.File{{Path: "cmd/gocov/oidc.go", Lines: "49-50, 57-58", Spans: [][2]int{{49, 50}, {57, 58}}}}
	if !reflect.DeepEqual(got.Diff.Uncovered, want) {
		t.Errorf("uncovered = %+v, want %+v", got.Diff.Uncovered, want)
	}
	if got.Diff.CoveredLines != 24 || got.Diff.TotalLines != 28 || !reflect.DeepEqual(got.Diff.UnmatchedFiles, []string{"docs/cli.md"}) {
		t.Errorf("diff = %+v", got.Diff)
	}
	// Nothing earlier to compare with.
	if got.Base != nil || got.Delta != nil {
		t.Errorf("base = %+v, delta = %v, want both null", got.Base, got.Delta)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=60" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestReportAPIBranchAndBaseline(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	earlier := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	seedCommitReport(t, f, &store.CommitReport{CommitSHA: "old1", Branch: "main", TotalPct: 70, PartCount: 1, UploadID: 1, CreatedAt: earlier, UpdatedAt: earlier})
	seedCommitReport(t, f, &store.CommitReport{CommitSHA: "new2", Branch: "main", TotalPct: 75, PartCount: 1, UploadID: 2})
	seedCommitReport(t, f, prReport())

	// No commit, no branch: the newest report on the default branch.
	got := decodeJSON[readapi.Report](t, get(f, "/api/v1/reports/bitbucket/acme/widgets"))
	if got.Commit != "new2" {
		t.Fatalf("commit = %q, want new2", got.Commit)
	}
	if got.Base == nil || got.Base.Commit != "old1" || got.Delta == nil || *got.Delta != 5 {
		t.Errorf("base = %+v, delta = %v, want old1 and +5", got.Base, got.Delta)
	}
	// One part: the upload's own page. Not a PR build: no diff.
	if got.ReportURL != "https://gocov.example/uploads/2" || got.Diff != nil {
		t.Errorf("report_url = %q, diff = %+v", got.ReportURL, got.Diff)
	}
	if got := decodeJSON[readapi.Report](t, get(f, "/api/v1/reports/bitbucket/acme/widgets?branch=feature")); got.Commit != "feed1234" {
		t.Errorf("?branch=feature gave %q", got.Commit)
	}
}

// What the read API may not serve looks exactly like what does not exist:
// the same 404, so it tells nobody which private repos are tracked.
func TestReportAPIServesOnlyPublicReports(t *testing.T) {
	for _, tc := range []struct {
		name       string
		visibility string
		instanceOn bool
		disabled   bool
	}{
		{"private repo", store.VisibilityPrivate, true, false},
		{"public reports off for the instance", store.VisibilityPublic, false, false},
		{"public reports off for the repo", store.VisibilityPublic, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublicFixture(t, tc.visibility, tc.instanceOn)
			f.repo.PublicReportsDisabled = tc.disabled
			if err := f.store.UpdateRepo(t.Context(), f.repo); err != nil {
				t.Fatal(err)
			}
			seedCommitReport(t, f, prReport())
			hidden := get(f, "/api/v1/reports/bitbucket/acme/widgets?commit=feed1234")
			missing := get(f, "/api/v1/reports/bitbucket/acme/nothing?commit=feed1234")
			if hidden.Code != http.StatusNotFound || hidden.Body.String() != missing.Body.String() {
				t.Errorf("hidden repo: %d %q, missing repo: %d %q; want the same 404",
					hidden.Code, hidden.Body, missing.Code, missing.Body)
			}
		})
	}
}

func TestReportAPIOpenInstanceServesEveryRepo(t *testing.T) {
	f := newFixture(t, nil) // no sign-in configured
	seedCommitReport(t, f, prReport())
	rec := get(f, "/api/v1/reports/bitbucket/acme/widgets?commit=feed1234")
	if rec.Code != http.StatusOK {
		t.Fatalf("open instance: status = %d: %s", rec.Code, rec.Body)
	}
	// Nothing public about it: no shared-cache policy.
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "public") {
		t.Errorf("Cache-Control = %q on an open instance", cc)
	}
}

func TestReportAPIUnknownCommitOrEmptyBranch(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	for _, path := range []string{
		"/api/v1/reports/bitbucket/acme/widgets?commit=nope",
		"/api/v1/reports/bitbucket/acme/widgets",
	} {
		if rec := get(f, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
	}
}

// getWith is get with a bearer credential.
func getWith(f *fixture, path, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

// requireSignIn turns an open-instance fixture into one with sign-in, where
// only a credential or the public rule opens a report.
func requireSignIn(f *fixture) {
	f.srv.auths["bitbucket"] = &fakeProvider{identity: memberIdentity()}
}

func TestReportAPIReadsAPrivateRepoWithItsCredential(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPrivate, true)
	seedCommitReport(t, f, prReport())
	other := &store.Repo{Forge: "bitbucket", Slug: "beta/gadgets", Token: "beta-token", DefaultBranch: "main"}
	if err := f.store.CreateRepo(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	const path = "/api/v1/reports/bitbucket/acme/widgets?commit=feed1234"

	for _, cred := range []string{"secret-token", "ws-secret"} {
		rec := getWith(f, path, cred)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d: %s", cred, rec.Code, rec.Body)
		}
		// A private answer: no shared cache may keep it.
		if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
			t.Errorf("%s: Cache-Control = %q", cred, cc)
		}
	}
	// Another repo's token opens nothing here: the same 404 as no token.
	if rec := getWith(f, path, "beta-token"); rec.Code != http.StatusNotFound {
		t.Errorf("another repo's token: status = %d, want 404", rec.Code)
	}
	// A token that is no token is said so, not read as anonymous.
	if rec := getWith(f, path, "nope"); rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown token: status = %d, want 401", rec.Code)
	}
}

func TestReportAPIPublicRepoWithAnotherReposToken(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	seedCommitReport(t, f, prReport())
	other := &store.Repo{Forge: "bitbucket", Slug: "beta/gadgets", Token: "beta-token", DefaultBranch: "main"}
	if err := f.store.CreateRepo(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	// The token grants nothing here, but anyone may read a public report.
	rec := getWith(f, "/api/v1/reports/bitbucket/acme/widgets?commit=feed1234", "beta-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=60" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestReportAPIGitHubOIDC(t *testing.T) {
	f, is := newOIDCFixture(t)
	requireSignIn(f)
	seedCommitReport(t, f, prReport())
	// A bitbucket repo of the same name, which a GitHub token never names.
	namesake := &store.Repo{Forge: "bitbucket", Slug: "acme/widgets", Token: "bb-token", DefaultBranch: "main"}
	if err := f.store.CreateRepo(t.Context(), namesake); err != nil {
		t.Fatal(err)
	}

	tok := is.mint(t, githubClaims("https://gocov.example"))
	if rec := getWith(f, "/api/v1/reports/github/acme/widgets?commit=feed1234", tok); rec.Code != http.StatusOK {
		t.Fatalf("own repo: status = %d: %s", rec.Code, rec.Body)
	}
	if rec := getWith(f, "/api/v1/reports/bitbucket/acme/widgets?commit=feed1234", tok); rec.Code != http.StatusNotFound {
		t.Errorf("other forge's namesake: status = %d, want 404", rec.Code)
	}
	claims := githubClaims("https://gocov.example")
	claims["repository"] = "acme/other"
	if rec := getWith(f, "/api/v1/reports/github/acme/widgets?commit=feed1234", is.mint(t, claims)); rec.Code != http.StatusNotFound {
		t.Errorf("another repo's token: status = %d, want 404", rec.Code)
	}
	rec := getWith(f, "/api/v1/reports/github/acme/widgets?commit=feed1234", is.mint(t, githubClaims("https://elsewhere.example")))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "oidc_bad_audience") {
		t.Errorf("wrong audience: %d %s", rec.Code, rec.Body)
	}
}

func TestReportAPIGitLabOIDC(t *testing.T) {
	f, is := newGitLabOIDCFixture(t, gitLabDotComIssuer, nil)
	requireSignIn(f)
	seedCommitReport(t, f, prReport())
	const path = "/api/v1/reports/gitlab/acme/widgets?commit=feed1234"
	if rec := getWith(f, path, is.mint(t, glClaims(gitLabDotComIssuer, "acme/widgets", "https://gocov.example"))); rec.Code != http.StatusOK {
		t.Fatalf("own project: status = %d: %s", rec.Code, rec.Body)
	}
	if rec := getWith(f, path, is.mint(t, glClaims(gitLabDotComIssuer, "acme/other", "https://gocov.example"))); rec.Code != http.StatusNotFound {
		t.Errorf("another project's token: status = %d, want 404", rec.Code)
	}
}

func TestReportAPIBitbucketOIDC(t *testing.T) {
	auds := []any{bbWorkspaceARI, "https://gocov.example"}
	const path = "/api/v1/reports/bitbucket/acme/widgets?commit=feed1234"

	t.Run("its own repo, checked with Bitbucket", func(t *testing.T) {
		f, is := newBitbucketOIDCFixture(t, bbRepoUUID)
		requireSignIn(f)
		seedCommitReport(t, f, prReport())
		if rec := getWith(f, path, is.mint(t, bbClaims(bbRepoUUID, auds))); rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		if len(f.forge.RepoIDCalls) != 1 {
			t.Errorf("got %d GetRepoID calls, want 1", len(f.forge.RepoIDCalls))
		}
	})
	t.Run("another repo's token", func(t *testing.T) {
		f, is := newBitbucketOIDCFixture(t, bbRepoUUID)
		requireSignIn(f)
		seedCommitReport(t, f, prReport())
		if rec := getWith(f, path, is.mint(t, bbClaims("{00000000-0000-0000-0000-000000000000}", auds))); rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})
	t.Run("a token naming no repo", func(t *testing.T) {
		f, is := newBitbucketOIDCFixture(t, bbRepoUUID)
		requireSignIn(f)
		claims := bbClaims(bbRepoUUID, auds)
		delete(claims, "repositoryUuid")
		delete(claims, "sub")
		if rec := getWith(f, path, is.mint(t, claims)); rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
		if len(f.forge.RepoIDCalls) != 0 {
			t.Errorf("asked Bitbucket %d times for nothing", len(f.forge.RepoIDCalls))
		}
	})
	t.Run("rate limited", func(t *testing.T) {
		f, is := newBitbucketOIDCFixture(t, bbRepoUUID)
		requireSignIn(f)
		for range maxTokenlessPerRepoHour {
			f.srv.tokenless.allow("acme/widgets", time.Now())
		}
		if rec := getWith(f, path, is.mint(t, bbClaims(bbRepoUUID, auds))); rec.Code != http.StatusTooManyRequests {
			t.Errorf("status = %d, want 429", rec.Code)
		}
	})
	t.Run("Bitbucket does not answer", func(t *testing.T) {
		f, is := newBitbucketOIDCFixture(t, bbRepoUUID)
		requireSignIn(f)
		f.forge.RepoIDErr = errors.New("boom")
		if rec := getWith(f, path, is.mint(t, bbClaims(bbRepoUUID, auds))); rec.Code != http.StatusBadGateway {
			t.Errorf("status = %d, want 502", rec.Code)
		}
	})
	t.Run("no workspace to ask through", func(t *testing.T) {
		f, is := newBitbucketOIDCFixture(t, bbRepoUUID)
		requireSignIn(f)
		loner := &store.Repo{Forge: "bitbucket", Slug: "loner/repo", Token: "loner-token", DefaultBranch: "main"}
		if err := f.store.CreateRepo(t.Context(), loner); err != nil {
			t.Fatal(err)
		}
		rec := getWith(f, "/api/v1/reports/bitbucket/loner/repo", is.mint(t, bbClaims(bbRepoUUID, auds)))
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "oidc_not_connected") {
			t.Errorf("status = %d: %s", rec.Code, rec.Body)
		}
	})
}

func TestReportAPIOIDCWithoutAVerifier(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPrivate, true)
	f.srv.oidc = nil // as on a server with no public URL
	rec := getWith(f, "/api/v1/reports/bitbucket/acme/widgets", "a.b.c")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "OIDC reads are not available") {
		t.Errorf("status = %d: %s", rec.Code, rec.Body)
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

// When the forge's signing keys cannot be fetched the token is neither good
// nor bad: say so, and that a retry may pass.
func TestReportAPIOIDCKeysUnreachable(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPrivate, true)
	is, _ := newIssuer(t, gitHubActionsIssuer, gitHubActionsIssuer+"/jwks")
	f.srv.oidc = oidc.New(oidc.Config{
		Audience:   "https://gocov.example",
		Issuers:    []string{gitHubActionsIssuer},
		HTTPClient: &http.Client{Transport: failingTransport{}},
	})
	rec := getWith(f, "/api/v1/reports/bitbucket/acme/widgets", is.mint(t, githubClaims("https://gocov.example")))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d: %s", rec.Code, rec.Body)
	}
}

// A store that fails is a 500 with nothing of the failure in it, by
// commit and by branch alike.
func TestReportAPIStoreFailures(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	f.srv.store = brokenStore{Store: f.store}
	for _, path := range []string{
		"/api/v1/reports/bitbucket/acme/widgets?commit=feed1234",
		"/api/v1/reports/bitbucket/acme/widgets?branch=main",
	} {
		if rec := get(f, path); rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "database") {
			t.Errorf("GET %s: %d %s", path, rec.Code, rec.Body)
		}
	}
}
