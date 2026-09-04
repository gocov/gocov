package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// testProvider serves both the OAuth and API endpoints from one fake host.
func testProvider(t *testing.T, mux *http.ServeMux) *Provider {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	p := New("the-key", "the-secret")
	p.AuthBaseURL = srv.URL + "/oauth"
	p.APIBaseURL = srv.URL + "/api"
	return p
}

func TestAuthorizeURL(t *testing.T) {
	p := New("the-key", "the-secret")
	u, err := url.Parse(p.AuthorizeURL("state123", "https://gocov.example/oauth/gitlab/callback"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u.String(), DefaultAuthBaseURL+"/authorize?") {
		t.Errorf("url = %s", u)
	}
	q := u.Query()
	if q.Get("client_id") != "the-key" || q.Get("state") != "state123" ||
		q.Get("scope") != "read_user read_api" ||
		q.Get("response_type") != "code" ||
		q.Get("redirect_uri") != "https://gocov.example/oauth/gitlab/callback" {
		t.Errorf("query = %v", q)
	}
}

func TestIdentity(t *testing.T) {
	mux := http.NewServeMux()
	var apiBase string

	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.PostForm.Get("client_id") != "the-key" || r.PostForm.Get("client_secret") != "the-secret" ||
			r.PostForm.Get("code") != "code99" || r.PostForm.Get("grant_type") != "authorization_code" {
			t.Errorf("form = %v", r.PostForm)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-abc"})
	})
	requireBearer := func(r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok-abc" {
			t.Errorf("%s auth = %q", r.URL.Path, got)
		}
	}
	mux.HandleFunc("GET /api/user", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(r)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 12345, "username": "janedev", "name": "Jane Dev", "email": "jane@example.com",
		})
	})
	mux.HandleFunc("GET /api/groups", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(r)
		switch r.URL.Query().Get("min_access_level") {
		case "10": // membership, not visibility
			if r.URL.Query().Get("page") == "2" {
				// Subgroups arrive as full paths — each is a workspace candidate.
				_ = json.NewEncoder(w).Encode([]map[string]any{{"full_path": "acme/platform"}})
				return
			}
			// First page links to a second one to exercise Link pagination.
			w.Header().Set("Link", "<"+apiBase+"/groups?page=2&min_access_level=10>; rel=\"next\"")
			_ = json.NewEncoder(w).Encode([]map[string]any{{"full_path": "widgets-inc"}})
		case "50": // Owner: the groups the account administers
			_ = json.NewEncoder(w).Encode([]map[string]any{{"full_path": "acme/platform"}})
		default:
			t.Errorf("min_access_level = %q, want 10 or 50", r.URL.Query().Get("min_access_level"))
		}
	})

	p := testProvider(t, mux)
	apiBase = p.APIBaseURL

	id, err := p.Identity(t.Context(), "code99", "https://gocov.example/oauth/gitlab/callback")
	if err != nil {
		t.Fatal(err)
	}
	if id.ForgeUUID != "12345" {
		t.Errorf("forge uuid = %q, want the numeric account id", id.ForgeUUID)
	}
	if id.DisplayName != "Jane Dev" || id.Email != "jane@example.com" {
		t.Errorf("identity = %+v", id)
	}
	// Group full paths across both pages, plus the username for
	// user-namespace projects.
	want := []string{"widgets-inc", "acme/platform", "janedev"}
	if !reflect.DeepEqual(id.Workspaces, want) {
		t.Errorf("workspaces = %v, want %v", id.Workspaces, want)
	}
	// Owned: the Owner-level group plus the personal namespace.
	if want := []string{"acme/platform", "janedev"}; !reflect.DeepEqual(id.OwnedWorkspaces, want) {
		t.Errorf("owned workspaces = %v, want %v", id.OwnedWorkspaces, want)
	}
}

func TestIdentityNoNameNoGroups(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok"})
	})
	mux.HandleFunc("GET /api/user", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 7, "username": "solo", "email": "solo@example.com",
		})
	})
	mux.HandleFunc("GET /api/groups", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})

	id, err := testProvider(t, mux).Identity(t.Context(), "c", "r")
	if err != nil {
		t.Fatal(err)
	}
	// No profile name: the username doubles as the display name; no
	// groups: the username alone is the workspace list.
	if id.Email != "solo@example.com" || id.DisplayName != "solo" {
		t.Errorf("identity = %+v", id)
	}
	if !reflect.DeepEqual(id.Workspaces, []string{"solo"}) {
		t.Errorf("workspaces = %v, want just the username", id.Workspaces)
	}
	if !reflect.DeepEqual(id.OwnedWorkspaces, []string{"solo"}) {
		t.Errorf("owned workspaces = %v, want just the username", id.OwnedWorkspaces)
	}
}

func TestIdentityErrors(t *testing.T) {
	newMux := func(handlers map[string]http.HandlerFunc) *http.ServeMux {
		mux := http.NewServeMux()
		for pattern, h := range handlers {
			mux.HandleFunc(pattern, h)
		}
		return mux
	}
	tokenOK := func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok"})
	}

	t.Run("exchange http error", func(t *testing.T) {
		mux := newMux(map[string]http.HandlerFunc{
			"POST /oauth/token": func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "bad", http.StatusUnauthorized)
			},
		})
		if _, err := testProvider(t, mux).Identity(t.Context(), "c", "r"); err == nil {
			t.Error("want error")
		}
	})
	t.Run("exchange error in 200 body", func(t *testing.T) {
		mux := newMux(map[string]http.HandlerFunc{
			"POST /oauth/token": func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
			},
		})
		_, err := testProvider(t, mux).Identity(t.Context(), "c", "r")
		if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
			t.Errorf("err = %v, want the forge error surfaced", err)
		}
	})
	t.Run("user without id", func(t *testing.T) {
		mux := newMux(map[string]http.HandlerFunc{
			"POST /oauth/token": tokenOK,
			"GET /api/user": func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"username": "x"})
			},
		})
		if _, err := testProvider(t, mux).Identity(t.Context(), "c", "r"); err == nil {
			t.Error("want error without an account id")
		}
	})
	t.Run("groups listing fails", func(t *testing.T) {
		mux := newMux(map[string]http.HandlerFunc{
			"POST /oauth/token": tokenOK,
			"GET /api/user": func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "username": "x", "email": "x@y"})
			},
			"GET /api/groups": func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "nope", http.StatusForbidden)
			},
		})
		if _, err := testProvider(t, mux).Identity(t.Context(), "c", "r"); err == nil {
			t.Error("want error when memberships cannot be listed")
		}
	})
}
