// What a signed-in user may see. Access is by workspace membership: a
// user may view the repos under the prefixes of the workspaces they belong
// to, unless the instance is open (no sign-in configured), in which case
// everything is visible.

package server

import (
	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/store"

	"net/http"
	"sort"
)

// userScope resolves the request user's workspace membership into a scope.
// Auth off → unscoped, so an open-mode instance behaves exactly as before
// M2. Auth on → the user's workspace prefixes; a missing user (which should
// not occur behind requireAuth) yields a deny-all scope rather than an open
// one.
func (s *Server) userScope(r *http.Request) (repoScope, error) {
	if !s.authEnabled() {
		return repoScope{scoped: false}, nil
	}
	prefixes := map[string]bool{}
	if u := currentUser(r); u != nil {
		wss, err := s.store.ListWorkspacesForUser(r.Context(), u.ID)
		if err != nil {
			return repoScope{}, err
		}
		for _, ws := range wss {
			prefixes[ws.Prefix] = true
		}
	}
	return repoScope{scoped: true, prefixes: prefixes}, nil
}

// repoScope captures which repos a request may see (M2/R3). When scoped is
// false the instance runs in open mode (D5) and every repo is visible;
// otherwise a repo is visible only when its workspace prefix is a member
// prefix.
type repoScope struct {
	scoped   bool
	prefixes map[string]bool
}

// allows reports whether a namespaced repo slug falls within the scope.
// Any slash-boundary prefix may carry the membership — a GitLab workspace
// registered at subgroup depth covers the projects below it.
func (rs repoScope) allows(slug string) bool {
	if !rs.scoped {
		return true
	}
	for _, prefix := range core.SlugPrefixes(slug) {
		if rs.prefixes[prefix] {
			return true
		}
	}
	return false
}

// canView reports whether the request may see the given repo slug. Callers
// that fail the check 404 (D3: a non-member must not learn a repo exists).
func (s *Server) canView(r *http.Request, slug string) (bool, error) {
	scope, err := s.userScope(r)
	if err != nil {
		return false, err
	}
	return scope.allows(slug), nil
}

// authorizeReport decides whether the request may see a repo's report
// pages — repo page, upload detail, source view, raw profile. Members (and
// open-mode instances) pass exactly as before. Anyone else passes only
// when the repo is effectively public: the forge reported it public, the
// repo's "Public reports" switch is on and the instance allows it
// (GOCOV_PUBLIC_REPORTS). A refused visitor gets reportNotFound's answer —
// the login redirect when signed out, the 404 page for a signed-in
// non-member of a non-public repo (D3) — so nothing about the repo leaks.
//
// ok is false when the refusal has been written. member reports whether
// the viewer passed by membership (or the instance being open) rather than
// through the public branch: the switch for member chrome like the
// settings button, which a signed-in stranger on a public repo must not
// see either.
func (s *Server) authorizeReport(w http.ResponseWriter, r *http.Request, repo *store.Repo) (member, ok bool) {
	allowed, err := s.canView(r, repo.Slug)
	if err != nil {
		s.internalError(w, "checking access", err)
		return false, false
	}
	if allowed {
		return true, true
	}
	if s.publicReports && repo.ReportsPublic() {
		// The cached "public" may have flipped on the forge; an aged
		// answer is re-verified in the background (mechanics in
		// core.ReverifyVisibilityIfStale) while this request still serves.
		s.pipeline.ReverifyVisibilityIfStale(repo)
		if currentUser(r) == nil {
			// The anonymous render is briefly cacheable — these pages are
			// the badge/SEO surface, so repeat crawler traffic should be
			// absorbed upstream. max-age stays short so turning the
			// "Public reports" switch off takes effect within a minute
			// even behind a shared cache, and Vary: Cookie keeps such a
			// cache from answering a signed-in member with the stored
			// anonymous variant.
			w.Header().Set("Cache-Control", "public, max-age=60")
			w.Header().Set("Vary", "Cookie")
		}
		return false, true
	}
	s.reportNotFound(w, r)
	return false, false
}

// reportNotFound answers a report-page lookup that found nothing. For an
// anonymous visitor the answer is the login redirect, exactly what a
// missing session got before public report pages existed — a 404 here
// would tell a signed-out browser which slugs and upload ids exist.
func (s *Server) reportNotFound(w http.ResponseWriter, r *http.Request) {
	if s.authEnabled() && currentUser(r) == nil {
		redirectToLogin(w, r)
		return
	}
	s.renderNotFound(w, r)
}

// publicView reports whether this render is the anonymous read-only view
// of a public repo's page — the only state the layout shows the sign-up
// band in. Report handlers call it after authorizeReport let the request
// through, so signed-out with auth enabled implies an effectively public
// repo.
func (s *Server) publicView(r *http.Request) bool {
	return s.authEnabled() && currentUser(r) == nil
}

// allowedWorkspaceSet is the D3 authorization rule: the operator's explicit
// GOCOV_ALLOWED_WORKSPACES list when set, otherwise the workspaces this
// instance tracks (registered workspace prefixes plus the workspace part
// of every registered repo slug).
func (s *Server) allowedWorkspaceSet(r *http.Request) (map[string]bool, error) {
	set := map[string]bool{}
	if len(s.allowedWorkspaces) > 0 {
		for _, ws := range s.allowedWorkspaces {
			set[ws] = true
		}
		return set, nil
	}
	workspaces, err := s.store.ListWorkspaces(r.Context())
	if err != nil {
		return nil, err
	}
	for _, ws := range workspaces {
		set[ws.Prefix] = true
	}
	repos, err := s.store.ListRepos(r.Context())
	if err != nil {
		return nil, err
	}
	for _, repo := range repos {
		for _, prefix := range core.SlugPrefixes(repo.Slug) {
			set[prefix] = true
		}
	}
	return set, nil
}

// trackedWorkspaces renders the allowed set for the login page, so it is
// obvious whose coverage an instance holds.
func (s *Server) trackedWorkspaces(r *http.Request) []string {
	set, err := s.allowedWorkspaceSet(r)
	if err != nil {
		return nil
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
