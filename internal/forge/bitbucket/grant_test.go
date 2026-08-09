package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bykclk/gocov/internal/forge"
)

func testConsumer(t *testing.T, handler http.HandlerFunc) *Consumer {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Consumer{
		Key: "ckey", Secret: "csecret",
		AuthBaseURL: srv.URL, APIBaseURL: srv.URL,
		HTTPClient: srv.Client(),
	}
}

func TestAuthorizeURL(t *testing.T) {
	c := &Consumer{Key: "ckey", Secret: "s"}
	u, err := url.Parse(c.AuthorizeURL("st4te", "https://gocov.example/oauth/bitbucket/callback/connect"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != "ckey" || q.Get("response_type") != "code" || q.Get("state") != "st4te" {
		t.Errorf("authorize query = %v", q)
	}
	if q.Get("redirect_uri") != "https://gocov.example/oauth/bitbucket/callback/connect" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	// No scope parameter: Bitbucket scopes live on the consumer.
	if q.Has("scope") {
		t.Error("authorize URL must not carry a scope parameter")
	}
}

func TestExchange(t *testing.T) {
	var gotGrantType, gotCode string
	c := testConsumer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/access_token":
			user, pass, _ := r.BasicAuth()
			if user != "ckey" || pass != "csecret" {
				t.Errorf("token auth = %s:%s, want consumer Basic auth", user, pass)
			}
			_ = r.ParseForm()
			gotGrantType, gotCode = r.PostFormValue("grant_type"), r.PostFormValue("code")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 7200,
				"scope": "repository pullrequest account",
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

	grant, err := c.Exchange(context.Background(), "thecode", "https://cb")
	if err != nil {
		t.Fatal(err)
	}
	if gotGrantType != "authorization_code" || gotCode != "thecode" {
		t.Errorf("grant_type/code = %q/%q", gotGrantType, gotCode)
	}
	if grant.Account != "covbot" || grant.AccessToken != "at-1" || grant.RefreshToken != "rt-1" {
		t.Errorf("grant = %+v", grant)
	}
	if grant.TTL.Hours() != 2 {
		t.Errorf("ttl = %v, want 2h", grant.TTL)
	}
}

func TestRefreshRotates(t *testing.T) {
	var gotRefresh string
	c := testConsumer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostFormValue("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q", r.PostFormValue("grant_type"))
		}
		gotRefresh = r.PostFormValue("refresh_token")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-2", "refresh_token": "rt-2", "expires_in": 7200,
		})
	})
	grant, err := c.Refresh(context.Background(), "rt-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotRefresh != "rt-1" {
		t.Errorf("sent refresh token %q", gotRefresh)
	}
	if grant.RefreshToken != "rt-2" || grant.AccessToken != "at-2" {
		t.Errorf("rotated grant = %+v, want the NEW refresh token surfaced", grant)
	}
}

func TestRefreshRevoked(t *testing.T) {
	c := testConsumer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid refresh_token"}`))
	})
	_, err := c.Refresh(context.Background(), "rt-dead")
	if !errors.Is(err, forge.ErrCredentialsRevoked) {
		t.Errorf("err = %v, want ErrCredentialsRevoked", err)
	}
}

func TestRefreshTransientError(t *testing.T) {
	c := testConsumer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := c.Refresh(context.Background(), "rt-1")
	if err == nil || errors.Is(err, forge.ErrCredentialsRevoked) {
		t.Errorf("err = %v, want a plain (transient) error", err)
	}
}

func TestForgeClientUsesBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	c := &Consumer{Key: "k", Secret: "s", APIBaseURL: srv.URL, HTTPClient: srv.Client()}

	fg := c.ForgeClient("at-1")
	err := fg.PostBuildStatus(context.Background(), "acme/widgets", "abc", forge.BuildStatus{
		State: forge.StateSuccessful, Name: "gocov", Key: "gocov/coverage",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer at-1" {
		t.Errorf("API auth = %q, want the grant's Bearer token (CHANGE-3052: header only)", gotAuth)
	}
	if strings.Contains(gotAuth, "Basic") {
		t.Error("grant client fell back to Basic auth")
	}
}
