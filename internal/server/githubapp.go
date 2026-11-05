package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/store"
)

// GitHub App connect flow (One-Click Connect). The app's Setup URL
// points at /github/setup: after an install GitHub redirects the
// installing browser here with ?installation_id=…. There is no webhook
// endpoint in v1 — the redirect is the only install signal, and uninstall
// is detected lazily when a token mint fails (upload.go).
//
// Linking is guarded twice: by the install state (handleGitHubSetup) or
// the user's confirmation, and by the trust model in linkInstallation.

// githubInstallCookie binds an install that gocov started to the browser
// that started it: GitHub passes the install page's state parameter on
// to the Setup URL, and /github/setup links the installation straight
// away only when that state matches this cookie.
const githubInstallCookie = "gocov_gh_install_state"

// handleGitHubInstall implements GET /github/install, where every
// "Install the gocov app" button points: it remembers a fresh state in a
// cookie and sends the browser to the App's install page carrying it.
func (s *Server) handleGitHubInstall(w http.ResponseWriter, r *http.Request) {
	if s.forges.GitHubApp == nil || !s.authEnabled() {
		http.NotFound(w, r)
		return
	}
	installURL := s.forges.InstallURL(r.Context())
	if installURL == "" {
		http.Error(w, "the GitHub App install page is unavailable right now; try again shortly", http.StatusServiceUnavailable)
		return
	}
	state := newState()
	setCookie(w, githubInstallCookie, state, stateCookieTTL, s.secureCookies)
	http.Redirect(w, r, installURL+"?state="+url.QueryEscape(state), http.StatusFound)
}

// githubInstallURL is where the app sends an owner to install the App:
// this server's /github/install, or empty when no install page is
// available.
func (s *Server) githubInstallURL(r *http.Request) string {
	if s.forges.InstallURL(r.Context()) == "" {
		return ""
	}
	return githubInstallPath
}

const githubInstallPath = "/github/install"

// handleGitHubSetup implements GET /github/setup. Linking an installation
// changes a workspace — and registering one creates a tenant — so it must
// be the user's own doing, not a link someone sent them: a GET reached by
// cross-site navigation carries the session cookie all the same. It goes
// ahead directly only as the end of an install this browser started from
// gocov (the state matches). Anything else — a crafted link, an install
// begun on GitHub, a retry — lands on onboarding with nothing changed,
// where the user confirms with a POST under the cross-origin guard.
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
		http.Redirect(w, r, connectOutcomeURL("install_requested", "", 0), http.StatusSeeOther)
		return
	}

	id, err := positiveInt(r.FormValue("installation_id"))
	if err != nil {
		http.Redirect(w, r, connectOutcomeURL("no_installation", "", 0), http.StatusSeeOther)
		return
	}
	if !s.installStateMatches(w, r) {
		http.Redirect(w, r, connectOutcomeURL("install_confirm", "", id), http.StatusSeeOther)
		return
	}
	dest, err := s.linkInstallation(r, u, id)
	if err != nil {
		s.internalError(w, "connecting github app", err)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// installStateMatches reports whether the request carries the state of an
// install this browser started (handleGitHubInstall), and spends it.
func (s *Server) installStateMatches(w http.ResponseWriter, r *http.Request) bool {
	c, err := r.Cookie(githubInstallCookie)
	if err != nil || c.Value == "" {
		return false
	}
	clearCookie(w, githubInstallCookie, s.secureCookies)
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.FormValue("state"))) == 1
}

// githubConnectInput is the body of POST /api/ui/github/connect.
type githubConnectInput struct {
	InstallationID int64 `json:"installation_id"`
}

// githubConnectResult says where the browser goes after a confirmed connect.
type githubConnectResult struct {
	Redirect string `json:"redirect"`
}

// handleAPIGitHubConnect implements POST /api/ui/github/connect: the
// user's own confirmation, from gocov's onboarding page, to link an
// installation GitHub sent them back with. Same checks as the setup
// redirect; the cross-origin guard on /api/ui is what makes it the
// user's doing.
func (s *Server) handleAPIGitHubConnect(w http.ResponseWriter, r *http.Request) {
	if s.forges.GitHubApp == nil || !s.authEnabled() {
		httpError(w, http.StatusNotFound, "not found")
		return
	}
	u := currentUser(r)
	if u == nil {
		httpError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	var in githubConnectInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.InstallationID <= 0 {
		httpError(w, http.StatusBadRequest, "installation_id must be a positive number")
		return
	}
	dest, err := s.linkInstallation(r, u, in.InstallationID)
	if err != nil {
		s.internalError(w, "connecting github app", err)
		return
	}
	s.writeJSON(w, githubConnectResult{Redirect: dest})
}

// linkInstallation links the installation to the account it lives on —
// an existing workspace, or a new one — under the user's authority, and
// returns where the browser goes next: the workspace, or onboarding with
// the outcome code explaining why not. An error is a server fault.
//
// Trust model: the installation id proves nothing — anyone can name any
// number. What is believed is (a) the GitHub API's answer, via app-JWT
// auth, about which account the installation lives on, and (b) the
// signed-in user's own membership/forge-workspace state, exactly the rules
// the registration flow enforces.
func (s *Server) linkInstallation(r *http.Request, u *store.User, id int64) (string, error) {
	login, err := s.forges.GitHubApp.InstallationAccount(r.Context(), id)
	if err != nil {
		s.log.Error("github app installation lookup", "installation", id, "err", err)
		return connectOutcomeURL("install_unconfirmed", "", id), nil
	}
	ws, err := s.store.WorkspaceByPrefix(r.Context(), "github", login)
	switch {
	case err == nil:
		return s.connectExisting(r, u, ws, login, id)
	case errors.Is(err, store.ErrNotFound):
		return s.connectNew(r, u, login, id)
	default:
		return "", fmt.Errorf("looking up workspace: %w", err)
	}
}

// connectExisting links the installation to an already-registered
// workspace. An owner's seat is required — with the register-flow
// concession: a user whose forge workspace list contains the
// prefix is seated on the spot, in the role the snapshot grants, instead
// of waiting for the next login sync. Installing the App is an org
// admin's move on GitHub too, so an owner arriving here is the normal
// case; a member arriving with an installation most likely became admin
// after their last sign-in, and signing in again refreshes the role.
func (s *Server) connectExisting(r *http.Request, u *store.User, ws *store.Workspace, login string, installationID int64) (string, error) {
	role, member, err := s.seat(r.Context(), u, ws)
	if err != nil {
		return "", fmt.Errorf("listing memberships: %w", err)
	}
	if !member {
		if !inForgeWorkspaces(u, login) {
			// Like every tenant surface: a non-member learns nothing
			// beyond what the conflict above already implies.
			s.log.Warn("github setup denied", "user", u.DisplayName, "installation", installationID, "account", login)
			return connectOutcomeURL("not_your_workspace", login, installationID), nil
		}
		if err := s.joinWorkspace(r, u, ws); err != nil {
			return "", fmt.Errorf("adding membership: %w", err)
		}
		role = forgeRole(u, login)
	}
	if role != store.RoleOwner {
		s.log.Warn("github setup denied to member", "user", u.DisplayName, "installation", installationID, "account", login)
		return connectOutcomeURL("owners_only", login, installationID), nil
	}
	ws.GitHubInstallationID = installationID
	ws.GitHubAppBroken = false
	if err := s.store.UpdateWorkspace(r.Context(), ws); err != nil {
		return "", fmt.Errorf("linking installation: %w", err)
	}
	s.log.Info("github app connected", "workspace", ws.Prefix, "installation", installationID, "user", u.DisplayName)
	return workspaceHomeURL(ws), nil
}

// connectNew is the install-first path: the account has no workspace here
// yet, so the registration claim rules apply — only accounts the user's forge list
// vouches for, and, since claiming creates the tenant and mints its
// token, only ones it says the user administers. The workspace is
// registered with the installation already linked, then the user lands
// on the setup page: the same activation moment as the register flow,
// minus the credentials step. Works in hosted and private mode alike;
// the forge snapshot is the only claim gate.
func (s *Server) connectNew(r *http.Request, u *store.User, login string, installationID int64) (string, error) {
	if inForgeWorkspaces(u, login) && forgeRole(u, login) != store.RoleOwner {
		s.log.Warn("github setup claim denied to member", "user", u.DisplayName, "installation", installationID, "account", login)
		return connectOutcomeURL("owners_only", login, installationID), nil
	}
	if !inForgeWorkspaces(u, login) {
		s.log.Warn("github setup claim denied", "user", u.DisplayName, "installation", installationID, "account", login)
		return connectOutcomeURL("not_your_workspace", login, installationID), nil
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
			return s.connectExisting(r, u, existing, login, installationID)
		}
		return "", fmt.Errorf("registering workspace: %w", err)
	}
	s.log.Info("workspace registered via github app", "prefix", login, "installation", installationID, "user", u.DisplayName)
	return workspaceHomeURL(ws), nil
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
	s.forges.MarkWorkspaceReposPrivate(ctx, ws)
	s.log.Info("github app disconnected", "workspace", ws.Prefix, "user", actor)
	return nil
}

// inForgeWorkspaces reports whether the login is in the user's stored
// forge workspace snapshot — the same server-side rule the register flow
// enforces.
func inForgeWorkspaces(u *store.User, login string) bool {
	if u.Forge != "github" {
		return false
	}
	return slices.Contains(u.ForgeWorkspaces, login)
}

// connectOutcomeURL sends an install that did not end in a connected
// workspace back to onboarding, which is the only screen an account in
// that state has. The query carries what the old connect page spelled out
// in prose: what happened, the account it was about, and the installation
// to come back to — enough for the app to write the message and both of
// its escape hatches, the org's OAuth-app policy page and a fresh
// sign-in that returns here (a brand-new org is missing from the sign-in
// snapshot the claim gate checks, since the OAuth token is dropped at
// login; re-auth refreshes it and the claim then goes through).
func connectOutcomeURL(outcome, login string, installationID int64) string {
	q := url.Values{"connect": {outcome}}
	if login != "" {
		q.Set("ws", login)
	}
	if installationID > 0 {
		q.Set("installation_id", strconv.FormatInt(installationID, 10))
	}
	return "/onboarding?" + q.Encode()
}
