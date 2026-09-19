package server

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/store"
)

// Workspace registration (M3/R2): a hosted-mode user claims a workspace
// the forge says they belong to. The picker is drawn from the forge
// workspace snapshot stored at login (D3) — never from a live forge call,
// because OAuth tokens are discarded at login.

// registerRow is one forge workspace on the onboarding picker.
type registerRow struct {
	Prefix string
	// State drives the row's control:
	//   "member"     registered here and the user is in — link to the index
	//   "registered" registered here but membership hasn't synced yet
	//                (the stored list is newer than the last login sync;
	//                fixed by signing in again)
	//   "unowned"    free, but the forge lists the user as a member, not
	//                an admin — creating a workspace is an owner's move
	//   "available"  free to register
	State string
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
		// (or answered the UI API with a 401) before this runs, so u is
		// non-nil past here.
		tenantNotFound(w, r)
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
	slices.Sort(prefixes)
	rows := make([]registerRow, 0, len(prefixes))
	for _, prefix := range prefixes {
		row := registerRow{Prefix: prefix, State: "available"}
		_, err := s.store.WorkspaceByPrefix(r.Context(), u.Forge, prefix)
		switch {
		case errors.Is(err, store.ErrNotFound):
			if forgeRole(u, prefix) != store.RoleOwner {
				row.State = "unowned"
			}
		case err != nil:
			return nil, err
		case member[prefix]:
			row.State = "member"
		default:
			row.State = "registered"
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// The two refusals a claim can meet, carried in the API's JSON error.
const (
	notListedMsg = "workspace is not in your forge account (sign in again if it is new)"
	notOwnerMsg  = "creating a workspace takes an admin or owner of it on the forge; ask one to register it, " +
		"or sign in again if you have just become one"
)

// errNotListed is claimOrJoin's answer when the prefix is not one the
// forge reported at login.
var errNotListed = errors.New("workspace is not in the user's forge account")

// claimOrJoin is the decision both registration surfaces make: check the
// prefix against the login snapshot, then either create the workspace or
// grant membership in the one already there. created says which happened.
func (s *Server) claimOrJoin(r *http.Request, u *store.User, prefix string) (ws *store.Workspace, created bool, err error) {
	// D2, enforced server-side: only workspaces the forge reported at
	// login are claimable, no matter what the request posts.
	if prefix == "" || !slices.Contains(u.ForgeWorkspaces, prefix) {
		return nil, false, errNotListed
	}
	fresh, existing, err := s.claimWorkspace(r, u, prefix)
	if err != nil {
		return nil, false, err
	}
	if fresh != nil {
		s.log.Info("workspace registered", "prefix", fresh.Prefix, "forge", fresh.Forge, "user", u.DisplayName)
		return fresh, true, nil
	}
	// Someone else registered it first — a non-event by construction (D2):
	// the forge says the user belongs, so membership is theirs; grant it
	// now instead of making them wait for the next login sync.
	if err := s.joinWorkspace(r, u, existing); err != nil {
		return nil, false, fmt.Errorf("adding membership: %w", err)
	}
	return existing, false, nil
}

// registerInput is the app's claim: one prefix off the picker.
type registerInput struct {
	Prefix string `json:"prefix"`
}

// registerResultDTO names the workspace the claim settled on, and whether
// this request was the one that created it — what the app routes on.
type registerResultDTO struct {
	Forge   string `json:"forge"`
	Prefix  string `json:"prefix"`
	Created bool   `json:"created"`
}

// handleAPIRegister implements POST /api/ui/onboarding/register: the
// picker's Create and Join, answering with the workspace instead of a
// redirect. The form's refusals in the app's terms — a prefix the forge
// never vouched for is not found, since the app only ever posts rows it
// was handed, and creating one still takes an owner there.
func (s *Server) handleAPIRegister(w http.ResponseWriter, r *http.Request) {
	u := s.registerUser(w, r)
	if u == nil {
		return
	}
	var in registerInput
	if !readJSON(w, r, &in) {
		return
	}
	prefix := strings.TrimSpace(in.Prefix)
	if prefix == "" {
		invalid(w, "Pick a workspace to register.")
		return
	}
	ws, created, err := s.claimOrJoin(r, u, prefix)
	switch {
	case errors.Is(err, errNotListed):
		httpError(w, http.StatusNotFound, "%s", notListedMsg)
		return
	case errors.Is(err, errNotOwner):
		httpError(w, http.StatusForbidden, "%s", notOwnerMsg)
		return
	case err != nil:
		s.internalError(w, "registering workspace", err)
		return
	}
	s.writeJSON(w, registerResultDTO{Forge: ws.Forge, Prefix: ws.Prefix, Created: created})
}

// errNotOwner is claimWorkspace's answer when the forge snapshot lists the
// user as a member of the prefix but not an admin of it.
var errNotOwner = errors.New("not an owner of the workspace on the forge")

// claimWorkspace registers prefix on the user's forge for the user,
// unless a workspace with that prefix already exists there (also when a
// concurrent claim wins the create race) — then it is returned as
// existing instead. Creating one
// takes an owner's role on the forge: it makes a tenant and mints its
// upload token, both owner-only from then on. Joining an existing one is
// open to any member.
func (s *Server) claimWorkspace(r *http.Request, u *store.User, prefix string) (created, existing *store.Workspace, err error) {
	existing, err = s.store.WorkspaceByPrefix(r.Context(), u.Forge, prefix)
	if err == nil {
		return nil, existing, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, nil, err
	}
	if forgeRole(u, prefix) != store.RoleOwner {
		return nil, nil, errNotOwner
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
		if existing, lookupErr := s.store.WorkspaceByPrefix(r.Context(), u.Forge, prefix); lookupErr == nil {
			return nil, existing, nil
		}
		return nil, nil, err
	}
	return ws, nil, nil
}

// joinWorkspace adds ws to the user's memberships, keeping the rest, in
// the role the forge snapshot grants — the same answer the next login
// sync would give.
func (s *Server) joinWorkspace(r *http.Request, u *store.User, ws *store.Workspace) error {
	memberships, err := s.store.ListMembershipsForUser(r.Context(), u.ID)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(memberships, func(m store.Membership) bool { return m.WorkspaceID == ws.ID }) {
		return nil // already a member
	}
	memberships = append(memberships, store.Membership{WorkspaceID: ws.ID, Role: forgeRole(u, ws.Prefix)})
	return s.store.SetUserMemberships(r.Context(), u.ID, memberships)
}
