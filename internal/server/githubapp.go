package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/store"
)

// GitHub App connect flow (One-Click Connect P1/D3). The app's Setup URL
// points at /github/setup: after an install GitHub redirects the
// installing browser here with ?installation_id=…. There is no webhook
// endpoint in v1 — the redirect is the only install signal, and uninstall
// is detected lazily when a token mint fails (upload.go).
//
// Trust model: the query string proves nothing — anyone can put any
// number in it. What the handler believes is (a) the GitHub API's answer,
// via app-JWT auth, about which account the installation lives on, and
// (b) the signed-in user's own membership/forge-workspace state, exactly
// the rules the M3 register flow enforces.

// handleGitHubSetup implements GET /github/setup.
func (s *Server) handleGitHubSetup(w http.ResponseWriter, r *http.Request) {
	if s.forges.GitHubApp == nil || !s.authEnabled() {
		http.NotFound(w, r)
		return
	}
	u := currentUser(r)
	if u == nil {
		// Unreachable behind requireAuth; kept as a guard against future
		// routing changes.
		redirectToLogin(w, r)
		return
	}

	if r.FormValue("setup_action") == "request" {
		// A member without admin rights asked the org owners to install;
		// there is no installation to link yet.
		s.connectOutcome(w, r, "install_requested", "", 0)
		return
	}

	id, err := strconv.ParseInt(r.FormValue("installation_id"), 10, 64)
	if err != nil || id <= 0 {
		s.connectOutcome(w, r, "no_installation", "", 0)
		return
	}
	login, err := s.forges.GitHubApp.InstallationAccount(r.Context(), id)
	if err != nil {
		s.log.Error("github app installation lookup", "installation", id, "err", err)
		s.connectOutcome(w, r, "install_unconfirmed", "", id)
		return
	}

	ws, err := s.store.WorkspaceByPrefix(r.Context(), "github", login)
	switch {
	case err == nil:
		s.connectExisting(w, r, u, ws, login, id)
	case errors.Is(err, store.ErrNotFound):
		s.connectNew(w, r, u, login, id)
	default:
		s.internalError(w, "looking up workspace", err)
	}
}

// connectExisting links the installation to an already-registered
// workspace. An owner's seat is required — with the register-flow
// concession (M3/D2): a user whose forge workspace list contains the
// prefix is seated on the spot, in the role the snapshot grants, instead
// of waiting for the next login sync. Installing the App is an org
// admin's move on GitHub too, so an owner arriving here is the normal
// case; a member arriving with an installation most likely became admin
// after their last sign-in, and signing in again refreshes the role.
func (s *Server) connectExisting(w http.ResponseWriter, r *http.Request, u *store.User, ws *store.Workspace, login string, installationID int64) {
	role, member, err := s.seat(r.Context(), u, ws)
	if err != nil {
		s.internalError(w, "listing memberships", err)
		return
	}
	if !member {
		if !inForgeWorkspaces(u, login) {
			// Like every tenant surface: a non-member learns nothing
			// beyond what the conflict above already implies.
			s.log.Warn("github setup denied", "user", u.DisplayName, "installation", installationID, "account", login)
			s.connectOutcome(w, r, "not_your_workspace", login, installationID)
			return
		}
		if err := s.joinWorkspace(r, u, ws); err != nil {
			s.internalError(w, "adding membership", err)
			return
		}
		role = forgeRole(u, login)
	}
	if role != store.RoleOwner {
		s.log.Warn("github setup denied to member", "user", u.DisplayName, "installation", installationID, "account", login)
		s.connectOutcome(w, r, "owners_only", login, installationID)
		return
	}
	ws.GitHubInstallationID = installationID
	ws.GitHubAppBroken = false
	if err := s.store.UpdateWorkspace(r.Context(), ws); err != nil {
		s.internalError(w, "linking installation", err)
		return
	}
	s.log.Info("github app connected", "workspace", ws.Prefix, "installation", installationID, "user", u.DisplayName)
	http.Redirect(w, r, workspaceHomeURL(ws), http.StatusSeeOther)
}

// connectNew is the install-first path: the account has no workspace here
// yet, so the M3 claim rules apply — only accounts the user's forge list
// vouches for, and, since claiming creates the tenant and mints its
// token, only ones it says the user administers. The workspace is
// registered with the installation already linked, then the user lands
// on the setup page: the same activation moment as the register flow,
// minus the credentials step. Works in hosted and private mode alike;
// the forge snapshot is the only claim gate.
func (s *Server) connectNew(w http.ResponseWriter, r *http.Request, u *store.User, login string, installationID int64) {
	if inForgeWorkspaces(u, login) && forgeRole(u, login) != store.RoleOwner {
		s.log.Warn("github setup claim denied to member", "user", u.DisplayName, "installation", installationID, "account", login)
		s.connectOutcome(w, r, "owners_only", login, installationID)
		return
	}
	if !inForgeWorkspaces(u, login) {
		s.log.Warn("github setup claim denied", "user", u.DisplayName, "installation", installationID, "account", login)
		s.connectOutcome(w, r, "not_your_workspace", login, installationID)
		return
	}
	token := core.NewToken()
	ws := &store.Workspace{
		Forge:                "github",
		Prefix:               login,
		Token:                token,
		DefaultBranch:        "main",
		GitHubInstallationID: installationID,
	}
	if err := s.store.RegisterWorkspace(r.Context(), ws, u.ID); err != nil {
		// A concurrent claim may have won the create race; link to it.
		if existing, lookupErr := s.store.WorkspaceByPrefix(r.Context(), "github", login); lookupErr == nil {
			s.connectExisting(w, r, u, existing, login, installationID)
			return
		}
		s.internalError(w, "registering workspace", err)
		return
	}
	s.log.Info("workspace registered via github app", "prefix", login, "installation", installationID, "user", u.DisplayName)
	http.Redirect(w, r, workspaceHomeURL(ws), http.StatusSeeOther)
}

// githubDisconnect is the GitHub half of the disconnect endpoint: forget
// the installation link. The installation itself lives on GitHub —
// uninstalling there is the org owner's move; this only stops gocov
// using it and drops resolution back to the credential chain.
func (s *Server) githubDisconnect(ctx context.Context, ws *store.Workspace, actor string) error {
	ws.GitHubInstallationID = 0
	ws.GitHubAppBroken = false
	if err := s.store.UpdateWorkspace(ctx, ws); err != nil {
		return fmt.Errorf("disconnecting github app: %w", err)
	}
	s.log.Info("github app disconnected", "workspace", ws.Prefix, "user", actor)
	return nil
}

// inForgeWorkspaces reports whether the login is in the user's stored
// forge workspace snapshot — the same server-side rule the register flow
// enforces (M3/D2).
func inForgeWorkspaces(u *store.User, login string) bool {
	if u.Forge != "github" {
		return false
	}
	return slices.Contains(u.ForgeWorkspaces, login)
}

// connectOutcome sends an install that did not end in a connected
// workspace back to onboarding, which is the only screen an account in
// that state has. The query carries what the old connect page spelled out
// in prose: what happened, the account it was about, and the installation
// to come back to — enough for the app to write the message and both of
// its escape hatches, the org's OAuth-app policy page and a fresh
// sign-in that returns here (a brand-new org is missing from the sign-in
// snapshot the claim gate checks, since the OAuth token is dropped at
// login (D3); re-auth refreshes it and the claim then goes through).
func (s *Server) connectOutcome(w http.ResponseWriter, r *http.Request, outcome, login string, installationID int64) {
	q := url.Values{"connect": {outcome}}
	if login != "" {
		q.Set("ws", login)
	}
	if installationID > 0 {
		q.Set("installation_id", strconv.FormatInt(installationID, 10))
	}
	http.Redirect(w, r, "/onboarding?"+q.Encode(), http.StatusSeeOther)
}
