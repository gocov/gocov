package gitlab

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gocov/gocov/internal/forge"
)

func testApplication(t *testing.T, handler http.HandlerFunc) *Application {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Application{
		Key: "akey", Secret: "asecret",
		AuthBaseURL: srv.URL, APIBaseURL: srv.URL,
		HTTPClient: srv.Client(),
	}
}

func TestGrantAuthorizeURL(t *testing.T) {
	a := &Application{Key: "akey", Secret: "s"}
	u, err := url.Parse(a.AuthorizeURL("st4te", "https://gocov.example/oauth/gitlab/callback"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != "akey" || q.Get("response_type") != "code" || q.Get("state") != "st4te" {
		t.Errorf("authorize query = %v", q)
	}
	if q.Get("redirect_uri") != "https://gocov.example/oauth/gitlab/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	// Connect asks for the write scope; sign-in's read-only consent is a
	// different request against the same application.
	if q.Get("scope") != "api" {
		t.Errorf("scope = %q, want api", q.Get("scope"))
	}
}

func TestGrantExchange(t *testing.T) {
	var gotGrantType, gotCode, gotKey, gotSecret string
	a := testApplication(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			gotGrantType, gotCode = r.PostFormValue("grant_type"), r.PostFormValue("code")
			gotKey, gotSecret = r.PostFormValue("client_id"), r.PostFormValue("client_secret")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 7200,
				"scope": "api",
			})
		case "/user":
			if got := r.Header.Get("Authorization"); got != "Bearer at-1" {
				t.Errorf("/user auth = %q", got)
			}
			_, _ = w.Write([]byte(`{"username":"covbot"}`))
		default:
			t.Errorf("unexpected call %s", r.URL.Path)
		}
	})

	grant, err := a.Exchange(t.Context(), "thecode", "https://cb")
	if err != nil {
		t.Fatal(err)
	}
	if gotGrantType != "authorization_code" || gotCode != "thecode" {
		t.Errorf("grant_type/code = %q/%q", gotGrantType, gotCode)
	}
	if gotKey != "akey" || gotSecret != "asecret" {
		t.Errorf("client credentials = %q/%q, want them in the form body", gotKey, gotSecret)
	}
	if grant.Account != "covbot" || grant.AccessToken != "at-1" || grant.RefreshToken != "rt-1" {
		t.Errorf("grant = %+v", grant)
	}
	if grant.TTL.Hours() != 2 {
		t.Errorf("ttl = %v, want 2h", grant.TTL)
	}
}

func TestGrantRefreshRotates(t *testing.T) {
	var gotRefresh, gotRedirect string
	a := testApplication(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostFormValue("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q", r.PostFormValue("grant_type"))
		}
		gotRefresh = r.PostFormValue("refresh_token")
		gotRedirect = r.PostFormValue("redirect_uri")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-2", "refresh_token": "rt-2", "expires_in": 7200,
		})
	})
	grant, err := a.Refresh(t.Context(), "rt-1", "https://cb")
	if err != nil {
		t.Fatal(err)
	}
	if gotRefresh != "rt-1" {
		t.Errorf("sent refresh token %q", gotRefresh)
	}
	// GitLab's token endpoint wants the redirect URI on refreshes too.
	if gotRedirect != "https://cb" {
		t.Errorf("redirect_uri = %q, want it on the refresh grant", gotRedirect)
	}
	if grant.RefreshToken != "rt-2" || grant.AccessToken != "at-2" {
		t.Errorf("rotated grant = %+v, want the NEW refresh token surfaced", grant)
	}
}

func TestGrantRefreshRevoked(t *testing.T) {
	a := testApplication(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"revoked"}`))
	})
	_, err := a.Refresh(t.Context(), "rt-dead", "https://cb")
	if !errors.Is(err, forge.ErrCredentialsRevoked) {
		t.Errorf("err = %v, want ErrCredentialsRevoked", err)
	}
}

func TestGrantRefreshTransientError(t *testing.T) {
	a := testApplication(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := a.Refresh(t.Context(), "rt-1", "https://cb")
	if err == nil || errors.Is(err, forge.ErrCredentialsRevoked) {
		t.Errorf("err = %v, want a plain (transient) error", err)
	}
}

func TestGrantNoExpiryDefaultsTTL(t *testing.T) {
	// Applications created before GitLab enabled token expiry answer
	// without expires_in; the cache still needs a finite TTL.
	a := testApplication(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "at-1", "refresh_token": "rt-1",
			})
		case "/user":
			_, _ = w.Write([]byte(`{"username":"covbot"}`))
		}
	})
	grant, err := a.Exchange(t.Context(), "c", "https://cb")
	if err != nil {
		t.Fatal(err)
	}
	if grant.TTL != grantTTLDefault {
		t.Errorf("ttl = %v, want the %v default", grant.TTL, grantTTLDefault)
	}
}

func TestGrantForgeClientUsesBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	a := &Application{Key: "k", Secret: "s", APIBaseURL: srv.URL, HTTPClient: srv.Client()}

	fg := a.ForgeClient("at-1")
	err := fg.PostBuildStatus(t.Context(), "acme/widgets", "abc", forge.BuildStatus{
		State: forge.StateSuccessful, Name: "gocov", Key: "gocov/coverage",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer at-1" {
		t.Errorf("API auth = %q, want the grant's Bearer token", gotAuth)
	}
}
