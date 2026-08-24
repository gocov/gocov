// What a signed-in user may see. Access is by workspace membership: a
// user may view the repos under the prefixes of the workspaces they belong
// to, unless the instance is open (no sign-in configured), in which case
// everything is visible.

package server

import (
	"net/http"
	"sort"
	"strings"
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
	for _, prefix := range slugPrefixes(slug) {
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
		for _, prefix := range slugPrefixes(repo.Slug) {
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

// slugPrefixes returns every slash-boundary prefix of a repo slug,
// longest first: "a/b/c" → ["a/b", "a"]. GitLab namespaces nest, so a
// repo's workspace can sit at any depth (a registered subgroup path is a
// workspace of its own); Bitbucket and GitHub slugs only ever have the
// single-segment prefix.
func slugPrefixes(slug string) []string {
	var out []string
	for i := len(slug); ; {
		j := strings.LastIndex(slug[:i], "/")
		if j < 0 {
			return out
		}
		out = append(out, slug[:j])
		i = j
	}
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
