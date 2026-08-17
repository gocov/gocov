package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gocov/gocov/internal/forge"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{
		BaseURL:    srv.URL,
		Token:      "tok",
		HTTPClient: srv.Client(),
	}
}

func TestPostBuildStatus(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath pins the API's URL-encoding trap: the project path
		// must arrive as a single %2F-joined segment, never as raw slashes.
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	})

	err := c.PostBuildStatus(context.Background(), "acme/widgets", "abc123", forge.BuildStatus{
		Key:         "gocov/coverage",
		State:       forge.StateSuccessful,
		Name:        "gocov",
		Description: "coverage: 80.0% (+1.2%)",
		URL:         "https://gocov.example/uploads/1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/projects/acme%2Fwidgets/statuses/abc123" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if gotBody["state"] != "success" || gotBody["name"] != "gocov" ||
		gotBody["description"] != "coverage: 80.0% (+1.2%)" ||
		gotBody["target_url"] != "https://gocov.example/uploads/1" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestPostBuildStatusSubgroupPath(t *testing.T) {
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusCreated)
	})
	err := c.PostBuildStatus(context.Background(), "grp/sub/proj", "sha", forge.BuildStatus{
		State: forge.StateSuccessful, Name: "gocov",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/projects/grp%2Fsub%2Fproj/statuses/sha" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestPostBuildStatusStates(t *testing.T) {
	var gotState string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotState = body["state"]
		w.WriteHeader(http.StatusCreated)
	})
	for state, want := range map[string]string{
		forge.StateSuccessful: "success",
		forge.StateFailed:     "failed",
		forge.StateInProgress: "pending",
	} {
		if err := c.PostBuildStatus(context.Background(), "a/b", "sha", forge.BuildStatus{State: state, Name: "gocov"}); err != nil {
			t.Fatal(err)
		}
		if gotState != want {
			t.Errorf("state %q mapped to %q, want %q", state, gotState, want)
		}
	}
	if err := c.PostBuildStatus(context.Background(), "a/b", "sha", forge.BuildStatus{State: "bogus"}); err == nil {
		t.Error("want error for unknown state")
	}
}

func TestPostBuildStatusRepostedStateIsNoError(t *testing.T) {
	// A merged re-upload posts the state the commit already has; GitLab
	// rejects the no-op transition with a 400 that must not surface.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "Cannot transition status via :run from :running (Reason(s): Status cannot transition via \"run\")"}`, http.StatusBadRequest)
	})
	err := c.PostBuildStatus(context.Background(), "a/b", "sha", forge.BuildStatus{State: forge.StateSuccessful, Name: "gocov"})
	if err != nil {
		t.Errorf("err = %v, want nil for a same-state repost", err)
	}
}

func TestPostBuildStatusHTTPError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "denied"}`, http.StatusForbidden)
	})
	err := c.PostBuildStatus(context.Background(), "a/b", "sha", forge.BuildStatus{State: forge.StateSuccessful})
	if err == nil {
		t.Fatal("want error on 403")
	}
}

func TestPostPRComment(t *testing.T) {
	var gotPath string
	var gotBody map[string]string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	})
	if err := c.PostPRComment(context.Background(), "acme/widgets", "42", "hello"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/projects/acme%2Fwidgets/merge_requests/42/notes" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["body"] != "hello" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestFindPRComment(t *testing.T) {
	// Notes are requested oldest first across pages; the newest match —
	// the last one seen — must win, and system notes never match.
	var srvURL string
	handler := func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("order_by") != "created_at" || q.Get("sort") != "asc" {
			t.Errorf("query = %q, want order_by=created_at&sort=asc", r.URL.RawQuery)
		}
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`[
				{"id": 30, "body": "**gocov** report for new", "system": false},
				{"id": 31, "body": "**gocov** added a commit", "system": true},
				{"id": 32, "body": "unrelated", "system": false}
			]`))
			return
		}
		w.Header().Set("Link", "<"+srvURL+r.URL.Path+"?page=2&order_by=created_at&sort=asc>; rel=\"next\"")
		_, _ = w.Write([]byte(`[
			{"id": 10, "body": "**gocov** report for old", "system": false},
			{"id": 11, "body": "a human comment", "system": false}
		]`))
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()
	srvURL = srv.URL
	c := &Client{BaseURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}

	id, err := c.FindPRComment(context.Background(), "acme/widgets", "42", "**gocov**")
	if err != nil {
		t.Fatal(err)
	}
	if id != "30" {
		t.Errorf("id = %q, want 30 (newest non-system match)", id)
	}
}

func TestFindPRCommentNoMatch(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id": 1, "body": "hi", "system": false}]`))
	})
	id, err := c.FindPRComment(context.Background(), "a/b", "1", "**gocov**")
	if err != nil || id != "" {
		t.Errorf("id, err = %q, %v; want empty, nil", id, err)
	}
}

func TestFindPRCommentHTTPError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	})
	if _, err := c.FindPRComment(context.Background(), "a/b", "1", "**gocov**"); err == nil {
		t.Error("want error on 401")
	}
}

func TestUpdatePRComment(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
	})
	if err := c.UpdatePRComment(context.Background(), "acme/widgets", "42", "31", "new body"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/projects/acme%2Fwidgets/merge_requests/42/notes/31" {
		t.Errorf("%s %s", gotMethod, gotPath)
	}
	if gotBody["body"] != "new body" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestGetPRDiff(t *testing.T) {
	// The changes API hands back per-file hunks without ---/+++ headers;
	// the client reassembles a unified diff the diffcov parser accepts.
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{
			"overflow": false,
			"changes": [
				{"old_path": "a.go", "new_path": "a.go", "deleted_file": false,
				 "diff": "@@ -1 +1,2 @@\n x\n+y\n"},
				{"old_path": "gone.go", "new_path": "gone.go", "deleted_file": true,
				 "diff": "@@ -1 +0,0 @@\n-z\n"},
				{"old_path": "old.go", "new_path": "new.go", "deleted_file": false,
				 "diff": "@@ -1 +1 @@\n-p\n+q"}
			]
		}`))
	})
	got, err := c.GetPRDiff(context.Background(), "acme/widgets", "42")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/projects/acme%2Fwidgets/merge_requests/42/changes" {
		t.Errorf("path = %q", gotPath)
	}
	want := "--- a/a.go\n+++ b/a.go\n@@ -1 +1,2 @@\n x\n+y\n" +
		"--- a/old.go\n+++ b/new.go\n@@ -1 +1 @@\n-p\n+q\n"
	if got != want {
		t.Errorf("diff = %q, want %q", got, want)
	}
}

func TestGetPRDiffOverflow(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"overflow": true, "changes": []}`))
	})
	if _, err := c.GetPRDiff(context.Background(), "a/b", "1"); err == nil {
		t.Error("want error on overflowed diff")
	}
}

func TestGetPRDiffHTTPError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	if _, err := c.GetPRDiff(context.Background(), "a/b", "1"); err == nil {
		t.Error("want error on 404")
	}
}

func TestGetDefaultBranch(t *testing.T) {
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"default_branch": "develop", "path_with_namespace": "acme/widgets"}`))
	})
	got, err := c.GetDefaultBranch(context.Background(), "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	if got != "develop" {
		t.Errorf("branch = %q", got)
	}
	if gotPath != "/projects/acme%2Fwidgets" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestGetDefaultBranchErrors(t *testing.T) {
	t.Run("404 maps to ErrRepoNotFound", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
		_, err := c.GetDefaultBranch(context.Background(), "a/ghost")
		if !errors.Is(err, forge.ErrRepoNotFound) {
			t.Errorf("err = %v, want ErrRepoNotFound", err)
		}
	})
	t.Run("http error", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusForbidden)
		})
		if _, err := c.GetDefaultBranch(context.Background(), "a/b"); err == nil {
			t.Error("want error on 403")
		}
	})
	t.Run("missing default_branch", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"path_with_namespace": "a/b"}`))
		})
		if _, err := c.GetDefaultBranch(context.Background(), "a/b"); err == nil {
			t.Error("want error when default_branch is absent")
		}
	})
}

func TestGetFileContent(t *testing.T) {
	var gotPath, gotRef string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotRef = r.URL.Query().Get("ref")
		_, _ = w.Write([]byte("package main\n"))
	})
	got, err := c.GetFileContent(context.Background(), "acme/widgets", "abc123", "cmd/app/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n" {
		t.Errorf("content = %q", got)
	}
	if gotPath != "/projects/acme%2Fwidgets/repository/files/cmd%2Fapp%2Fmain.go/raw" {
		t.Errorf("path = %q", gotPath)
	}
	if gotRef != "abc123" {
		t.Errorf("ref = %q", gotRef)
	}
}

func TestGetFileContentNotFound(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	_, err := c.GetFileContent(context.Background(), "a/b", "sha", "ghost.go")
	if !errors.Is(err, forge.ErrRepoNotFound) {
		t.Errorf("err = %v, want ErrRepoNotFound", err)
	}
}

func TestPublishReportNotImplemented(t *testing.T) {
	// No check-run equivalent on GitLab (D3): the upload flow reports the
	// sentinel as "skipped" and the MR note carries the diff table.
	c := &Client{BaseURL: "http://unused", Token: "tok"}
	err := c.PublishReport(context.Background(), "a/b", "sha", forge.Report{Title: "t"}, nil)
	if !errors.Is(err, forge.ErrNotImplemented) {
		t.Errorf("err = %v, want ErrNotImplemented", err)
	}
}

func TestFactoryValidation(t *testing.T) {
	if _, err := Factory(nil); err == nil {
		t.Error("want error without credentials")
	}
	if _, err := Factory(map[string]string{"username": "u"}); err == nil {
		t.Error("want error without token")
	}
	f, err := Factory(map[string]string{"token": "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if f.(*Client).BaseURL != DefaultBaseURL {
		t.Errorf("base URL = %q", f.(*Client).BaseURL)
	}
	// D1: a self-managed instance's base URL is honored from day one.
	f, err = Factory(map[string]string{"token": "tok", "base_url": "https://git.corp.example/api/v4"})
	if err != nil {
		t.Fatal(err)
	}
	if f.(*Client).BaseURL != "https://git.corp.example/api/v4" {
		t.Errorf("base URL = %q", f.(*Client).BaseURL)
	}
}

func TestNextLink(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{`<https://gitlab.com/api/v4/x?page=2>; rel="next", <https://gitlab.com/api/v4/x?page=5>; rel="last"`,
			"https://gitlab.com/api/v4/x?page=2"},
		{`<https://gitlab.com/api/v4/x?page=1>; rel="prev"`, ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := nextLink(tt.header); got != tt.want {
			t.Errorf("nextLink(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}
