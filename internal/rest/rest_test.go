package rest

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"slices"
	"strings"
	"testing"
	"time"
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

func TestBodyAndTokenShapes(t *testing.T) {
	err := &Error{Status: http.StatusBadRequest, Body: `{"message":"Cannot transition status"}`, msg: "x"}
	if Body(err) != err.Body {
		t.Errorf("Body(refusal) = %q, want the answer's explanation", Body(err))
	}
	if Body(errors.New("other")) != "" || Body(nil) != "" {
		t.Error("Body must be empty for non-refusals")
	}
	if got := (Token{ExpiresIn: 7200}).TTL(); got != 2*time.Hour {
		t.Errorf("TTL = %v, want 2h", got)
	}
	if got := (Token{}).TTL(); got != 0 {
		t.Errorf("TTL without expires_in = %v, want 0 so callers can fall back", got)
	}
}

func TestEscapePathKeepsSlashes(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a/b/c.go", "a/b/c.go"},
		{"dir name/file#1?.go", "dir%20name/file%231%3F.go"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := EscapePath(tt.in); got != tt.want {
			t.Errorf("EscapePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// EachPage follows the Link header page by page, stops at the cap and
// says so, and stops at the first error.
func TestEachPage(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "boom" {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		n := len(page) // "", "x", "xx": three pages, then the end
		if n < 2 {
			w.Header().Set("Link", `<`+srv.URL+`/items?page=`+page+`x>; rel="next"`)
		}
		_, _ = fmt.Fprintf(w, "[%d, %d]", 2*n, 2*n+1)
	}))
	defer srv.Close()
	c := &Client{Name: "test", BaseURL: srv.URL, HTTPClient: srv.Client()}

	walk := func(start string, maxPages int) ([]int, bool, error) {
		var got []int
		truncated, err := EachPage(t.Context(), c, start, maxPages, func(page []int) { got = append(got, page...) })
		return got, truncated, err
	}
	if got, truncated, err := walk("/items", 5); err != nil || truncated || !slices.Equal(got, []int{0, 1, 2, 3, 4, 5}) {
		t.Errorf("all pages = %v, truncated %v, err %v", got, truncated, err)
	}
	if got, truncated, err := walk("/items", 2); err != nil || !truncated || !slices.Equal(got, []int{0, 1, 2, 3}) {
		t.Errorf("capped at 2 = %v, truncated %v, err %v; want 4 items and truncated", got, truncated, err)
	}
	if got, truncated, err := walk("/items", 3); err != nil || truncated || len(got) != 6 {
		t.Errorf("cap equal to the listing = %v, truncated %v, err %v; want every item, not truncated", got, truncated, err)
	}
	if got, _, err := walk("/items?page=boom", 5); err == nil || got != nil {
		t.Errorf("failing page = %v, err %v; want the error and nothing handed on", got, err)
	}
}

// EachValuesPage follows the body's "next" URL, stops at the cap and says
// so, and stops early when the callback has what it needs.
func TestEachValuesPage(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page") // "", "x", "xx": three pages
		n := len(page)
		next := ""
		if n < 2 {
			next = srv.URL + "/items?page=" + page + "x"
		}
		_, _ = fmt.Fprintf(w, `{"values": [%d, %d], "next": %q}`, 2*n, 2*n+1, next)
	}))
	defer srv.Close()
	c := &Client{Name: "test", BaseURL: srv.URL, HTTPClient: srv.Client()}

	walk := func(maxPages int, stopAt int) ([]int, bool, error) {
		var got []int
		truncated, err := EachValuesPage(t.Context(), c, "/items", maxPages, func(values []int) bool {
			got = append(got, values...)
			return !slices.Contains(values, stopAt)
		})
		return got, truncated, err
	}
	if got, truncated, err := walk(5, -1); err != nil || truncated || !slices.Equal(got, []int{0, 1, 2, 3, 4, 5}) {
		t.Errorf("all pages = %v, truncated %v, err %v", got, truncated, err)
	}
	if got, truncated, err := walk(2, -1); err != nil || !truncated || len(got) != 4 {
		t.Errorf("capped at 2 = %v, truncated %v, err %v; want 4 items and truncated", got, truncated, err)
	}
	if got, truncated, err := walk(5, 3); err != nil || truncated || !slices.Equal(got, []int{0, 1, 2, 3}) {
		t.Errorf("stopped early = %v, truncated %v, err %v; want the first two pages only", got, truncated, err)
	}
}
