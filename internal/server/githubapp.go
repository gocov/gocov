package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gocov/gocov/internal/forge"
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
	if s.githubApp == nil || !s.authEnabled() {
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
		s.renderConnect(w, r, http.StatusOK, "Install requested",
			"Your request went to the organization owners. Once an owner approves the "+
				"installation, GitHub sends them back here and the workspace gets connected.", "")
		return
	}

	id, err := strconv.ParseInt(r.FormValue("installation_id"), 10, 64)
	if err != nil || id <= 0 {
		s.renderConnect(w, r, http.StatusBadRequest, "Missing installation",
			"This page is where GitHub returns after installing the gocov app; it cannot be "+
				"used on its own. Start from the install page instead.", s.githubInstallURL(r.Context()))
		return
	}
	login, err := s.githubApp.InstallationAccount(r.Context(), id)
	if err != nil {
		s.log.Error("github app installation lookup", "installation", id, "err", err)
		s.renderConnect(w, r, http.StatusBadGateway, "GitHub did not confirm the installation",
			"The installation could not be verified with GitHub. If you just installed the "+
				"app, try reloading this page in a moment.", "")
		return
	}

	ws, err := s.store.WorkspaceByPrefix(r.Context(), login)
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
// workspace. Membership is required — with the register-flow concession
// (M3/D2): a user whose forge workspace list contains the prefix is made
// a member on the spot instead of waiting for the next login sync.
func (s *Server) connectExisting(w http.ResponseWriter, r *http.Request, u *store.User, ws *store.Workspace, login string, installationID int64) {
	if ws.Forge != "github" {
		// Prefixes are forge-agnostic and globally unique (M3); the name
		// belongs to another forge's tenant here.
		s.renderConnect(w, r, http.StatusConflict, "Workspace name in use",
			"The name "+login+" is already registered under another forge on this server, "+
				"so the installation cannot be linked to it.", "")
		return
	}
	member, err := s.isMember(r.Context(), u, ws)
	if err != nil {
		s.internalError(w, "listing memberships", err)
		return
	}
	if !member {
		if !inForgeWorkspaces(u, login) {
			// Like every tenant surface: a non-member learns nothing
			// beyond what the conflict above already implies.
			s.log.Warn("github setup denied", "user", u.DisplayName, "installation", installationID, "account", login)
			s.renderConnect(w, r, http.StatusForbidden, "Not your workspace",
				"You are not a member of the "+login+" workspace on this server. If you just "+
					"joined the organization on GitHub, sign out and back in, then retry.", "")
			return
		}
		if err := s.joinWorkspace(r, u, ws); err != nil {
			s.internalError(w, "adding membership", err)
			return
		}
	}
	ws.GitHubInstallationID = installationID
	ws.GitHubAppBroken = false
	if err := s.store.UpdateWorkspace(r.Context(), ws); err != nil {
		s.internalError(w, "linking installation", err)
		return
	}
	s.log.Info("github app connected", "workspace", ws.Prefix, "installation", installationID, "user", u.DisplayName)
	http.Redirect(w, r, "/workspaces/"+ws.Prefix+"?connected=1", http.StatusSeeOther)
}

// connectNew is the install-first path: the account has no workspace here
// yet, so the M3 claim rules apply — hosted mode only, and only accounts
// the user's forge list vouches for. The workspace is registered with the
// installation already linked, then the user lands on the setup page: the
// same activation moment as the register flow, minus the credentials step.
func (s *Server) connectNew(w http.ResponseWriter, r *http.Request, u *store.User, login string, installationID int64) {
	if !s.hosted {
		s.renderConnect(w, r, http.StatusNotFound, "Workspace not registered",
			"This server has no workspace named "+login+". Register it first "+
				"(gocov-server workspace add), then install the app again.", "")
		return
	}
	if !inForgeWorkspaces(u, login) {
		s.log.Warn("github setup claim denied", "user", u.DisplayName, "installation", installationID, "account", login)
		s.renderConnect(w, r, http.StatusForbidden, "Not your workspace",
			"GitHub reports the installation on "+login+", which is not among your "+
				"organizations on this account. Sign in again if you joined it recently.", "")
		return
	}
	token, err := newToken()
	if err != nil {
		s.internalError(w, "generating workspace token", err)
		return
	}
	ws := &store.Workspace{
		Forge:                "github",
		Prefix:               login,
		Token:                token,
		DefaultBranch:        "main",
		GitHubInstallationID: installationID,
	}
	if err := s.store.RegisterWorkspace(r.Context(), ws, u.ID); err != nil {
		// A concurrent claim may have won the create race; link to it.
		if existing, lookupErr := s.store.WorkspaceByPrefix(r.Context(), login); lookupErr == nil {
			s.connectExisting(w, r, u, existing, login, installationID)
			return
		}
		s.internalError(w, "registering workspace", err)
		return
	}
	s.log.Info("workspace registered via github app", "prefix", login, "installation", installationID, "user", u.DisplayName)
	http.Redirect(w, r, "/workspaces/"+ws.Prefix+"/setup", http.StatusSeeOther)
}

// handleGitHubDisconnect implements POST /workspaces/{prefix}/github/disconnect:
// forget the installation link. The installation itself lives on GitHub —
// uninstalling there is the org owner's move; this only stops gocov
// using it and drops resolution back to the credential chain.
func (s *Server) handleGitHubDisconnect(w http.ResponseWriter, r *http.Request) {
	ws := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	ws.GitHubInstallationID = 0
	ws.GitHubAppBroken = false
	if err := s.store.UpdateWorkspace(r.Context(), ws); err != nil {
		s.internalError(w, "disconnecting github app", err)
		return
	}
	s.log.Info("github app disconnected", "workspace", ws.Prefix, "user", currentUser(r).DisplayName)
	http.Redirect(w, r, "/workspaces/"+ws.Prefix+"?saved=1", http.StatusSeeOther)
}

// isMember reports whether the user is a member of the workspace.
func (s *Server) isMember(ctx context.Context, u *store.User, ws *store.Workspace) (bool, error) {
	memberOf, err := s.store.ListWorkspacesForUser(ctx, u.ID)
	if err != nil {
		return false, err
	}
	for _, m := range memberOf {
		if m.ID == ws.ID {
			return true, nil
		}
	}
	return false, nil
}

// inForgeWorkspaces reports whether the login is in the user's stored
// forge workspace snapshot — the same server-side rule the register flow
// enforces (M3/D2).
func inForgeWorkspaces(u *store.User, login string) bool {
	if u.Forge != "github" {
		return false
	}
	for _, ws := range u.ForgeWorkspaces {
		if ws == login {
			return true
		}
	}
	return false
}

// renderConnect renders the connect flow's terminal states that have no
// workspace page to land on.
func (s *Server) renderConnect(w http.ResponseWriter, r *http.Request, code int, heading, message, installURL string) {
	w.WriteHeader(code)
	s.render(w, r, "connect.html", map[string]any{
		"Heading":    heading,
		"Message":    message,
		"InstallURL": installURL,
	})
}

// githubInstallURL resolves the app's public install page, best effort: a
// GitHub hiccup must not take a settings page down with it. Empty string
// when unavailable; templates then render the state without a link.
func (s *Server) githubInstallURL(ctx context.Context) string {
	if s.githubApp == nil {
		return ""
	}
	u, err := s.githubApp.InstallURL(ctx)
	if err != nil {
		s.log.Warn("github app install url", "err", err)
		return ""
	}
	return u
}

// installationForge returns the App-backed client when the workspace is
// connected to a GitHub App installation — the top link of the credential
// chain (D4), which also makes check runs first-class (the App is never
// hit by the classic-PAT 403). A refused mint marks the connection broken
// (lazy uninstall detection, D3) and returns nil, so the upload degrades
// exactly like missing credentials: skipped, never failed, with stored
// tokens still honored further down the chain. A transient mint failure
// only logs and falls through the same way.
func (s *Server) installationForge(ctx context.Context, ws *store.Workspace, forgeName string) forge.Forge {
	if s.githubApp == nil || ws == nil || forgeName != "github" ||
		ws.Forge != "github" || ws.GitHubInstallationID == 0 {
		return nil
	}
	fg, err := s.githubApp.ForgeClient(ctx, ws.GitHubInstallationID)
	if err != nil {
		if errors.Is(err, forge.ErrCredentialsRevoked) {
			s.markAppBroken(ctx, ws, err)
		} else {
			s.log.Warn("github app installation token", "workspace", ws.Prefix, "err", err)
		}
		return nil
	}
	if ws.GitHubAppBroken {
		// Minting works again (a lifted suspension, a restored key) —
		// heal the flag so settings stops asking for a reconnect.
		ws.GitHubAppBroken = false
		if err := s.store.UpdateWorkspace(ctx, ws); err != nil {
			s.log.Error("clearing github app broken flag", "workspace", ws.Prefix, "err", err)
		}
	}
	return fg
}

// markAppBroken records that the workspace's installation stopped
// working so the settings page can show "reconnect" (D3). The id is
// kept — only flagged — since a reinstall arrives through the setup
// redirect and overwrites it anyway.
func (s *Server) markAppBroken(ctx context.Context, ws *store.Workspace, cause error) {
	s.log.Warn("github app installation revoked", "workspace", ws.Prefix,
		"installation", ws.GitHubInstallationID, "err", cause)
	if ws.GitHubAppBroken {
		return
	}
	ws.GitHubAppBroken = true
	if err := s.store.UpdateWorkspace(ctx, ws); err != nil {
		s.log.Error("marking github app broken", "workspace", ws.Prefix, "err", err)
	}
}
