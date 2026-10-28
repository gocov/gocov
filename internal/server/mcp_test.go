package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gocov/gocov/internal/auth"
	"github.com/gocov/gocov/internal/readapi"
	"github.com/gocov/gocov/internal/store"
)

// callMCP posts one get_report call to /mcp with the given access token
// (none when empty) and returns the tool's text and whether it is a tool
// error.
func callMCP(t *testing.T, f *fixture, token, args string) (string, bool) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_report","arguments":` + args + `}}`
	rec := postMCP(f, token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Result.Content) != 1 {
		t.Fatalf("answer %s: %v", rec.Body, err)
	}
	return resp.Result.Content[0].Text, resp.Result.IsError
}

// postMCP posts one message to /mcp, with the access token when one is
// given.
func postMCP(f *fixture, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

// strangerToken stores an MCP access token for a signed-up user who
// belongs to no workspace: what a hosted instance's passer-by holds.
func strangerToken(t *testing.T, f *fixture) string {
	t.Helper()
	u := &store.User{Forge: "github", ForgeUUID: "stranger", DisplayName: "Stranger"}
	if err := f.store.UpsertUser(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	token := mcpTokenPrefix + "stranger"
	if err := f.store.CreateMCPToken(t.Context(), &store.MCPToken{
		TokenHash: hashToken(token), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return token
}

func TestMCPReadsAPublicReport(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	seedCommitReport(t, f, prReport())

	// A user of no workspace: the public rule opens it.
	tok := strangerToken(t, f)
	text, isErr := callMCP(t, f, tok, `{"forge":"bitbucket","repo":"acme/widgets","commit":"feed1234"}`)
	want := "bitbucket/acme/widgets @ feed123 (feature, PR 42)\ncoverage: 80.0%, 2 parts\n" +
		"diff coverage: 85.7% (24/28 changed lines)\nuncovered changed lines:\n  cmd/gocov/oidc.go: 49-50, 57-58\n" +
		"changed files without coverage data: docs/cli.md\nreport: https://gocov.example/uploads/7?parts=merged\n"
	if isErr || text != want {
		t.Errorf("text (error %v):\n%s\nwant:\n%s", isErr, text, want)
	}

	// The same answer as the read API, as JSON.
	text, _ = callMCP(t, f, tok, `{"forge":"bitbucket","repo":"/acme/widgets/","branch":"feature","format":"json"}`)
	var rep readapi.Report
	if err := json.Unmarshal([]byte(text), &rep); err != nil || rep.Commit != "feed1234" || len(rep.Diff.Uncovered) != 1 {
		t.Errorf("json answer %q: %v", text, err)
	}
}

// A repo the read may not serve answers exactly like one that does not
// exist, so the tool tells nobody which private repos are tracked.
func TestMCPHidesWhatItMayNotServe(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPrivate, true)
	seedCommitReport(t, f, prReport())
	tok := strangerToken(t, f)
	hidden, isErr := callMCP(t, f, tok, `{"forge":"bitbucket","repo":"acme/widgets"}`)
	if !isErr || !strings.Contains(hidden, "not public") {
		t.Errorf("private repo: error %v, %q", isErr, hidden)
	}
	// The same words, but for the name, as a repo that is not tracked.
	if missing, _ := callMCP(t, f, tok, `{"forge":"bitbucket","repo":"acme/nothing"}`); strings.Replace(missing, "nothing", "widgets", 1) != hidden {
		t.Errorf("private %q and missing %q answer differently", hidden, missing)
	}
}

func TestMCPOpenInstanceServesEveryRepo(t *testing.T) {
	f := newFixture(t, nil) // no sign-in configured
	seedCommitReport(t, f, prReport())
	if text, isErr := callMCP(t, f, "", `{"forge":"bitbucket","repo":"acme/widgets","commit":"feed1234"}`); isErr {
		t.Errorf("open instance: %s", text)
	}
}

func TestMCPToolErrors(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	tok := strangerToken(t, f)
	for _, tc := range []struct{ name, args, want string }{
		{"no repo named", `{}`, "name the repo"},
		{"no arguments at all", `null`, "name the repo"},
		{"arguments of the wrong type", `{"repo":5}`, "invalid arguments"},
		{"no report for the commit", `{"forge":"bitbucket","repo":"acme/widgets","commit":"nope"}`, "no report for commit nope; the commit's CI must upload"},
		{"no report on the branch", `{"forge":"bitbucket","repo":"acme/widgets"}`, "no report on branch main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, isErr := callMCP(t, f, tok, tc.args)
			if !isErr || !strings.Contains(text, tc.want) {
				t.Errorf("error %v, %q; want it to mention %q", isErr, text, tc.want)
			}
		})
	}
}

// brokenStore fails the reads /mcp makes.
type brokenStore struct {
	store.Store
	failRepo bool
}

func (b brokenStore) RepoBySlug(ctx context.Context, forge, slug string) (*store.Repo, error) {
	if b.failRepo {
		return nil, errors.New("database down")
	}
	return b.Store.RepoBySlug(ctx, forge, slug)
}

func (b brokenStore) CommitReport(context.Context, int64, string) (*store.CommitReport, error) {
	return nil, errors.New("database down")
}

func (b brokenStore) ListBranchCommitReports(context.Context, int64, string, int) ([]*store.CommitReport, error) {
	return nil, errors.New("database down")
}

func TestMCPStoreFailures(t *testing.T) {
	for _, failRepo := range []bool{true, false} {
		f := newPublicFixture(t, store.VisibilityPublic, true)
		tok := strangerToken(t, f)
		f.srv.store = brokenStore{Store: f.store, failRepo: failRepo}
		text, isErr := callMCP(t, f, tok, `{"forge":"bitbucket","repo":"acme/widgets","commit":"feed1234"}`)
		// The model reads the tool's text: no database detail in it.
		if !isErr || text != "internal error" {
			t.Errorf("failRepo=%v: error %v, %q", failRepo, isErr, text)
		}
	}
}

func TestMCPRoute(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	f.srv = New(Config{Store: f.store, Blobs: f.blobs, BaseURL: "https://gocov.example", Version: "v9.9.9",
		Auths: []auth.Provider{&fakeProvider{identity: memberIdentity()}}, PublicReports: true})
	tok := strangerToken(t, f)
	// GET, which would open a server stream, is refused by the transport.
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /mcp: status = %d", rec.Code)
	}
	if rec := postMCP(f, tok, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`); !strings.Contains(rec.Body.String(), `"version":"v9.9.9"`) {
		t.Errorf("initialize: %s", rec.Body)
	}
}

// Without a public URL there is no issuer to sign clients in by: /mcp
// serves public reports to anyone, as before MCP sign-in, and an MCP
// client's OAuth discovery gets the plain 404 that says so.
func TestMCPWithoutBaseURLServesPublicReports(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	f.srv = New(Config{Store: f.store, Blobs: f.blobs,
		Auths: []auth.Provider{&fakeProvider{identity: memberIdentity()}}, PublicReports: true})
	seedCommitReport(t, f, prReport())
	if text, isErr := callMCP(t, f, "", `{"forge":"bitbucket","repo":"acme/widgets","commit":"feed1234"}`); isErr {
		t.Errorf("public report: %s", text)
	}
	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-authorization-server",
	} {
		if rec := get(f, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, Location %q", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// Signed out, any other well-known path is a plain 404, not the login
// redirect.
func TestWellKnownIsAPlainNotFound(t *testing.T) {
	f := newPublicFixture(t, store.VisibilityPublic, true)
	for _, path := range []string{"/.well-known/openid-configuration", "/.well-known/security.txt"} {
		if rec := get(f, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, Location %q", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}
