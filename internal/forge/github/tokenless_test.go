package github

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// tokenlessAPI is the canned GitHub side of a VerifyRunClaim call: the
// token mint plus the three lookups, each mutable per test case.
type tokenlessAPI struct {
	repo map[string]any
	run  map[string]any
	pr   map[string]any
	// missing 404s the given paths ("/repos/acme/widgets", ...).
	missing map[string]bool
}

func okTokenlessAPI() *tokenlessAPI {
	return &tokenlessAPI{
		repo: map[string]any{"private": false},
		run: map[string]any{
			"status": "in_progress", "event": "pull_request",
			"head_sha": "abc123", "run_attempt": 2,
			"repository": map[string]any{"full_name": "acme/widgets"},
		},
		pr: map[string]any{
			"state": "open",
			"head": map[string]any{
				"sha":  "abc123",
				"repo": map[string]any{"full_name": "forker/widgets"},
			},
		},
		missing: map[string]bool{},
	}
}

func (a *tokenlessAPI) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/access_tokens") {
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "itok", "expires_at": "2099-01-01T00:00:00Z"})
			return
		}
		if a.missing[r.URL.Path] {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		switch r.URL.Path {
		case "/repos/acme/widgets":
			body = a.repo
		case "/repos/acme/widgets/actions/runs/9001":
			body = a.run
		case "/repos/acme/widgets/pulls/42":
			body = a.pr
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer itok" {
			t.Errorf("lookup %s authenticated with %q, want the installation token", r.URL.Path, got)
		}
		_ = json.NewEncoder(w).Encode(body)
	}
}

func okClaim() RunClaim {
	return RunClaim{
		RepoSlug: "acme/widgets", RunID: 9001, RunAttempt: 2,
		PRNumber: 42, HeadSHA: "abc123", HeadRepo: "forker/widgets",
	}
}

func TestVerifyRunClaim(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(api *tokenlessAPI, claim *RunClaim)
		reject string // substring of the rejection reason; "" = accepted
	}{
		{"valid claim", func(*tokenlessAPI, *RunClaim) {}, ""},
		{"slug casing differs", func(api *tokenlessAPI, claim *RunClaim) {
			api.run["repository"] = map[string]any{"full_name": "Acme/Widgets"}
			api.pr["head"].(map[string]any)["repo"] = map[string]any{"full_name": "Forker/Widgets"}
		}, ""},
		{"private repo", func(api *tokenlessAPI, _ *RunClaim) {
			api.repo["private"] = true
		}, "public repos only"},
		{"repo invisible to installation", func(api *tokenlessAPI, _ *RunClaim) {
			api.missing["/repos/acme/widgets"] = true
		}, "cannot see"},
		{"run not found", func(api *tokenlessAPI, _ *RunClaim) {
			api.missing["/repos/acme/widgets/actions/runs/9001"] = true
		}, "not found"},
		{"run of another repository", func(api *tokenlessAPI, _ *RunClaim) {
			api.run["repository"] = map[string]any{"full_name": "other/repo"}
		}, "another repository"},
		{"run is not a pull_request event", func(api *tokenlessAPI, _ *RunClaim) {
			api.run["event"] = "push"
		}, "only pull_request runs"},
		{"run already completed", func(api *tokenlessAPI, _ *RunClaim) {
			api.run["status"] = "completed"
		}, "still in progress"},
		{"run builds another commit", func(api *tokenlessAPI, _ *RunClaim) {
			api.run["head_sha"] = "fff999"
		}, "not building commit"},
		{"stale run attempt", func(api *tokenlessAPI, _ *RunClaim) {
			api.run["run_attempt"] = 3
		}, "attempt 3, not 2"},
		{"pr not found", func(api *tokenlessAPI, _ *RunClaim) {
			api.missing["/repos/acme/widgets/pulls/42"] = true
		}, "not found"},
		{"pr closed", func(api *tokenlessAPI, _ *RunClaim) {
			api.pr["state"] = "closed"
		}, "not open"},
		{"pr head moved on", func(api *tokenlessAPI, _ *RunClaim) {
			api.pr["head"].(map[string]any)["sha"] = "fff999"
		}, "head is not abc123"},
		{"pr head on another fork", func(api *tokenlessAPI, _ *RunClaim) {
			api.pr["head"].(map[string]any)["repo"] = map[string]any{"full_name": "elsewhere/widgets"}
		}, "head is not on forker/widgets"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := okTokenlessAPI()
			claim := okClaim()
			tc.mutate(api, &claim)
			app := testApp(t, api.handler(t))

			err := app.VerifyRunClaim(t.Context(), 77, claim)
			rejected, definitive := errors.AsType[*ClaimRejectedError](err)
			switch {
			case tc.reject == "" && err != nil:
				t.Fatalf("claim rejected: %v", err)
			case tc.reject != "" && !definitive:
				t.Fatalf("got %v, want a ClaimRejectedError", err)
			case tc.reject != "" && !strings.Contains(rejected.Reason, tc.reject):
				t.Fatalf("rejection %q does not mention %q", rejected.Reason, tc.reject)
			}
		})
	}
}

// A GitHub 5xx is transport trouble worth retrying, not a verdict on the
// claim — it must not come back as a ClaimRejectedError.
func TestVerifyRunClaimTransientError(t *testing.T) {
	app := testApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/access_tokens") {
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "itok", "expires_at": "2099-01-01T00:00:00Z"})
			return
		}
		http.Error(w, "boom", http.StatusBadGateway)
	})
	err := app.VerifyRunClaim(t.Context(), 77, okClaim())
	if err == nil {
		t.Fatal("5xx verified successfully")
	}
	if _, definitive := errors.AsType[*ClaimRejectedError](err); definitive {
		t.Fatalf("5xx surfaced as a definitive rejection: %v", err)
	}
}
