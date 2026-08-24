package server

import (
	"errors"
	"net/http"
	"slices"
	"sort"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/store"
)

// Workspace registration (M3/R2): a hosted-mode user claims a workspace
// the forge says they belong to. The page renders from the forge
// workspace snapshot stored at login (D3) — never from a live forge call,
// because OAuth tokens are discarded at login.

// registerRow is one forge workspace on the registration page.
type registerRow struct {
	Prefix string
	// State drives the row's control:
	//   "member"     registered here and the user is in — link to the index
	//   "registered" registered here but membership hasn't synced yet
	//                (the stored list is newer than the last login sync;
	//                fixed by signing in again)
	//   "taken"      the prefix is registered under another forge; slugs
	//                are forge-agnostic, so the name is unavailable
	//   "available"  free to register
	State string
	// TakenBy names the other forge holding the prefix when State is
	// "taken" — with three forges the collision is no longer exotic, so
	// the row says exactly who has the name.
	TakenBy string
}

// registerUser gates both registration routes: hosted mode only (a private
// instance has no registration UI, D1) and a signed-in user required.
func (s *Server) registerUser(w http.ResponseWriter, r *http.Request) *store.User {
	u := currentUser(r)
	if u == nil {
		// Registration derives its claimable workspaces from the signed-in
		// forge identity, so it needs sign-in configured — hosted mode
		// always has it, a private instance only when a provider is set.
		// With no provider the UI is open and there is no identity to
		// register from; 404 rather than a login loop. When sign-in is on,
		// requireAuth has already redirected an anonymous visitor to /login
		// before this runs, so u is non-nil past here.
		http.NotFound(w, r)
		return nil
	}
	return u
}

// registerRows resolves the user's stored forge workspaces against the
// tracked ones. Registered-by-others-and-member cannot appear as a fourth
// state: login sync already made the user a member of any tracked
// workspace their forge list contains (D2).
func (s *Server) registerRows(r *http.Request, u *store.User) ([]registerRow, error) {
	memberOf, err := s.store.ListWorkspacesForUser(r.Context(), u.ID)
	if err != nil {
		return nil, err
	}
	member := make(map[string]bool, len(memberOf))
	for _, ws := range memberOf {
		member[ws.Prefix] = true
	}

	prefixes := append([]string(nil), u.ForgeWorkspaces...)
	sort.Strings(prefixes)
	rows := make([]registerRow, 0, len(prefixes))
	for _, prefix := range prefixes {
		row := registerRow{Prefix: prefix, State: "available"}
		ws, err := s.store.WorkspaceByPrefix(r.Context(), prefix)
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return nil, err
		case ws.Forge != u.Forge:
			row.State = "taken"
			row.TakenBy = providerLabel(ws.Forge)
		case member[prefix]:
			row.State = "member"
		default:
			row.State = "registered"
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// handleRegisterPage implements GET /register. The onboarding wizard's
// Workspace step is now the claim surface (it shows the same picker), so
// this route only preserves the old URL by redirecting there. The signed-in
// gate is unchanged; it works in both hosted and private mode once a
// sign-in provider is configured.
func (s *Server) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	if s.registerUser(w, r) == nil {
		return
	}
	http.Redirect(w, r, "/onboarding", http.StatusFound)
}

// handleRegister implements POST /register: it creates the workspace and
// its first membership atomically, then shows the upload token — the only
// time it is ever rendered (D5: afterwards rotate-only).
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	u := s.registerUser(w, r)
	if u == nil {
		return
	}
	prefix := r.FormValue("prefix")

	// D2, enforced server-side: only workspaces the forge reported at
	// login are claimable, no matter what the form posts.
	inForgeList := slices.Contains(u.ForgeWorkspaces, prefix)
	if prefix == "" || !inForgeList {
		http.Error(w, "workspace is not in your forge account (sign in again if it is new)", http.StatusForbidden)
		return
	}

	created, existing, err := s.claimWorkspace(r, u, prefix)
	if err != nil {
		s.internalError(w, "registering workspace", err)
		return
	}
	if created != nil {
		s.log.Info("workspace registered", "prefix", created.Prefix, "forge", created.Forge, "user", u.DisplayName)
		// Land on the wizard's "workspace ready" state (D6): the reporting
		// capability card, then Continue to the CI step.
		http.Redirect(w, r, onboardingReadyURL(created.Prefix), http.StatusSeeOther)
		return
	}

	if existing.Forge != u.Forge {
		// Slugs are forge-agnostic ("prefix/repo" is the only key), so the
		// same name on two forges cannot coexist as separate tenants.
		http.Error(w, "this workspace name is already registered under "+providerLabel(existing.Forge)+
			" on this server, so it is unavailable here", http.StatusConflict)
		return
	}
	// Someone else registered it first — a non-event by construction (D2):
	// the forge says the user belongs, so membership is theirs; grant it
	// now instead of making them wait for the next login sync.
	if err := s.joinWorkspace(r, u, existing); err != nil {
		s.internalError(w, "adding membership", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// claimWorkspace registers prefix for the user, unless a workspace with
// that prefix already exists (also when a concurrent claim wins the
// create race) — then it is returned as existing instead.
func (s *Server) claimWorkspace(r *http.Request, u *store.User, prefix string) (created, existing *store.Workspace, err error) {
	existing, err = s.store.WorkspaceByPrefix(r.Context(), prefix)
	if err == nil {
		return nil, existing, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, nil, err
	}
	token, err := core.NewToken()
	if err != nil {
		return nil, nil, err
	}
	ws := &store.Workspace{
		Forge:         u.Forge,
		Prefix:        prefix,
		Token:         token,
		DefaultBranch: "main",
	}
	if err := s.store.RegisterWorkspace(r.Context(), ws, u.ID); err != nil {
		if existing, lookupErr := s.store.WorkspaceByPrefix(r.Context(), prefix); lookupErr == nil {
			return nil, existing, nil
		}
		return nil, nil, err
	}
	return ws, nil, nil
}

// joinWorkspace adds ws to the user's memberships, keeping the rest.
func (s *Server) joinWorkspace(r *http.Request, u *store.User, ws *store.Workspace) error {
	memberOf, err := s.store.ListWorkspacesForUser(r.Context(), u.ID)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(memberOf)+1)
	for _, m := range memberOf {
		if m.ID == ws.ID {
			return nil // already a member
		}
		ids = append(ids, m.ID)
	}
	return s.store.SetUserWorkspaces(r.Context(), u.ID, append(ids, ws.ID))
}
