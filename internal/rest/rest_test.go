package rest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"
)

func TestNextLink(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{`<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=5>; rel="last"`,
			"https://api.github.com/x?page=2"},
		{`<https://api.github.com/x?page=1>; rel="prev"`, ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NextLink(tt.header); got != tt.want {
			t.Errorf("NextLink(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestRefusalIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"nope"}`, http.StatusForbidden)
	}))
	defer srv.Close()
	c := &Client{Name: "test", BaseURL: srv.URL, HTTPClient: srv.Client()}

	err := c.Get(t.Context(), "/thing", nil)
	if Status(err) != http.StatusForbidden {
		t.Fatalf("Status(%v) = %d, want 403", err, Status(err))
	}
	e, ok := errors.AsType[*Error](err)
	if !ok || !strings.Contains(e.Body, "nope") || !strings.Contains(err.Error(), "test: /thing returned 403") {
		t.Errorf("err = %#v", err)
	}
	if Status(errors.New("other")) != 0 || Status(nil) != 0 {
		t.Error("Status must be 0 for non-refusals")
	}
}

func TestGetBytesBounds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/x-diff" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(strings.Repeat("x", 10)))
	}))
	defer srv.Close()
	c := &Client{Name: "test", BaseURL: srv.URL, HTTPClient: srv.Client()}

	if got, err := c.GetBytes(t.Context(), "/d", "text/x-diff", 10); err != nil || len(got) != 10 {
		t.Errorf("got %d bytes, err %v; want 10, nil", len(got), err)
	}
	if _, err := c.GetBytes(t.Context(), "/d", "text/x-diff", 9); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("err = %v, want the size error", err)
	}
}

func TestSendShapesBody(t *testing.T) {
	var gotBody, gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 64)
		n, _ := r.Body.Read(b)
		gotBody, gotType = string(b[:n]), r.Header.Get("Content-Type")
		w.Header().Set("Link", `<`+"http://next"+`>; rel="next"`)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := &Client{Name: "test", BaseURL: srv.URL, HTTPClient: srv.Client()}

	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.JSON(t.Context(), http.MethodPost, "/x", map[string]int{"a": 1}, &out); err != nil || !out.OK {
		t.Fatalf("JSON: %v, out %+v", err, out)
	}
	if gotBody != `{"a":1}` || gotType != "application/json" {
		t.Errorf("body %q type %q", gotBody, gotType)
	}
	if err := c.Send(t.Context(), http.MethodDelete, "/x", nil); err != nil || gotBody != "" || gotType != "" {
		t.Errorf("nil payload must send no body: %v body %q type %q", err, gotBody, gotType)
	}
	next, err := c.GetPage(t.Context(), "/x", &out)
	if err != nil || next != "http://next" {
		t.Errorf("GetPage next = %q, err %v", next, err)
	}
}

func TestPostFormIsATokenEndpointCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		if r.Method != http.MethodPost || user != "id" || pass != "secret" ||
			r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" ||
			r.Header.Get("Accept") != "application/json" {
			t.Errorf("request = %s %v auth %s:%s", r.Method, r.Header, user, pass)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		switch r.PostForm.Get("code") {
		case "good":
			_, _ = w.Write([]byte(`{"access_token":"tok"}`))
		default:
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	c := &Client{Name: "test", BaseURL: srv.URL, HTTPClient: srv.Client(), Authorize: Basic("id", "secret")}

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := c.PostForm(t.Context(), "/token", neturl.Values{"code": {"good"}}, &tok); err != nil || tok.AccessToken != "tok" {
		t.Fatalf("PostForm = %+v, %v", tok, err)
	}
	// A refusal keeps the endpoint's own error code readable.
	err := c.PostForm(t.Context(), srv.URL+"/token", neturl.Values{"code": {"bad"}}, &tok)
	if e, ok := errors.AsType[*Error](err); !ok || e.Status != http.StatusBadRequest || !strings.Contains(e.Body, "invalid_grant") {
		t.Errorf("refusal = %#v", err)
	}
}
