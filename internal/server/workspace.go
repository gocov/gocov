package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/hosted"
	"github.com/gocov/gocov/internal/store"
)

// Workspace settings (M3/R3) — the way workspaces are administered:
// token rotation, default branch, one-click forge connection and gate
// defaults — and the setup screen (R4) next to it. Both are the app's
// screens over the endpoints here; their page routes serve the shell and
// leave every access question to this file. Both are members-only;
// anyone else 404s (like every other tenant surface, a non-member must
// not learn the workspace exists). Within the workspace the role
// decides: a member reads, an owner — the role the forge's own
// admin/owner role maps to at sign-in — changes things. Every mutation
// and the upload token are owner-only; a member who asks anyway gets a
// 403, since the workspace's existence is no secret to them.

// memberWorkspace resolves the {forge}/{prefix} path segments to a
// workspace the signed-in user is a member of, writing a 404 otherwise,
// and returns the user's role in it. With auth off there are no members,
// so the pages do not exist.
func (s *Server) memberWorkspace(w http.ResponseWriter, r *http.Request) (*store.Workspace, store.Role) {
	u := s.signedIn(w, r)
	if u == nil {
		return nil, ""
	}
	ws, err := s.store.WorkspaceByPrefix(r.Context(), r.PathValue("forge"), r.PathValue("prefix"))
	if errors.Is(err, store.ErrNotFound) {
		tenantNotFound(w, r)
		return nil, ""
	}
	if err != nil {
		s.internalError(w, "loading workspace", err)
		return nil, ""
	}
	role, member, err := s.seat(r.Context(), u, ws)
	if err != nil {
		s.internalError(w, "listing memberships", err)
		return nil, ""
	}
	if !member {
		tenantNotFound(w, r)
		return nil, ""
	}
	return ws, role
}

// tenantNotFound is how a member-only surface answers for a workspace or
// repo that is missing — or that is there but the viewer may not see, so
// that a non-member cannot tell the two apart (D3). Pages get net/http's
// plain 404; the UI API gets the same answer in JSON.
func tenantNotFound(w http.ResponseWriter, r *http.Request) {
	if apiUIPath(r.URL.Path) {
		httpError(w, http.StatusNotFound, "not found")
		return
	}
	http.NotFound(w, r)
}

// signedIn is the gate on the member-only pages: the signed-in user, or
// nil after writing a 404 — with sign-in off there are no members, so
// the pages do not exist, and a signed-out request must not learn that
// they would.
func (s *Server) signedIn(w http.ResponseWriter, r *http.Request) *store.User {
	if !s.authEnabled() {
		tenantNotFound(w, r)
		return nil
	}
	u := currentUser(r)
	if u == nil {
		tenantNotFound(w, r)
	}
	return u
}

// ownerWorkspace is memberWorkspace for the owner-only routes: a member
// who is not an owner gets a 403 instead of the page.
func (s *Server) ownerWorkspace(w http.ResponseWriter, r *http.Request) *store.Workspace {
	ws, role := s.memberWorkspace(w, r)
	if ws == nil {
		return nil
	}
	if role != store.RoleOwner {
		ownersOnly(w, r)
		return nil
	}
	return ws
}

// seat reports the user's role in the workspace and whether they are a
// member of it at all.
func (s *Server) seat(ctx context.Context, u *store.User, ws *store.Workspace) (store.Role, bool, error) {
	memberships, err := s.store.ListMembershipsForUser(ctx, u.ID)
	if err != nil {
		return "", false, err
	}
	for _, m := range memberships {
		if m.WorkspaceID == ws.ID {
			return m.Role, true, nil
		}
	}
	return "", false, nil
}

// ownersOnly answers a member's attempt at an owner-only action. The
// pages hide these controls from members, so this is the answer to a
// hand-built request, not to a click.
func ownersOnly(w http.ResponseWriter, r *http.Request) {
	const msg = "only workspace owners can do this — on the forge, that is an admin or owner of the workspace"
	if apiUIPath(r.URL.Path) {
		httpError(w, http.StatusForbidden, "%s", msg)
		return
	}
	http.Error(w, msg, http.StatusForbidden)
}

// forgeConnection reads the workspace's per-forge connection columns:
// whether a connection is recorded (a GitHub App installation or a stored
// grant), whether it is flagged broken, and the account its posts carry
// (empty for the GitHub App, which posts as gocov[bot]). It is the one
// place that knows which columns belong to which forge.
func forgeConnection(ws *store.Workspace) (connected, broken bool, account string) {
	switch ws.Forge {
	case "github":
		return ws.GitHubInstallationID != 0, ws.GitHubAppBroken, ""
	case "bitbucket", "gitlab":
		return ws.Grant.Account != "", ws.Grant.Broken, ws.Grant.Account
	}
	return false, false, ""
}

// reportingState is the workspace's connection as one word — on, off or
// broken — with the account its posts carry (empty for the GitHub App,
// which posts as gocov[bot]).
func reportingState(ws *store.Workspace) (state, account string) {
	connected, broken, account := forgeConnection(ws)
	switch {
	case broken:
		return "broken", account
	case connected:
		return "on", account
	}
	return "off", account
}

// reportingAvailable reports whether this deployment has a one-click
// connect mechanism for the workspace's forge at all: the GitHub App, or
// the Bitbucket/GitLab consent grant. Without one the card explains that
// reporting needs stored credentials instead.
func (s *Server) reportingAvailable(ws *store.Workspace) bool {
	if ws.Forge == "github" {
		return s.forges.GitHubApp != nil
	}
	g := connectGrantFor(ws.Forge)
	return g != nil && s.forges.Connector(g.forge) != nil
}

// connectURL is where an owner starts connecting the workspace: GitHub's
// App installation page, or this server's consent-grant route for the
// forges that use one. Empty when the deployment offers neither.
func (s *Server) connectURL(r *http.Request, ws *store.Workspace) string {
	if !s.reportingAvailable(ws) {
		return ""
	}
	if ws.Forge == "github" {
		return s.forges.InstallURL(r.Context())
	}
	return workspaceConnectURL(ws)
}

// maskSecret renders a token as its last eight characters behind a run of
// bullets, matching the Reveal control's masked form.
func maskSecret(tok string) string {
	const tail = 8
	if len(tok) <= tail {
		return strings.Repeat("•", len(tok))
	}
	return strings.Repeat("•", 40) + tok[len(tok)-tail:]
}

// The coverage-gate rules, named as the settings endpoints carry them and
// labeled as the validation message names them.
var gateRuleLabels = map[string]string{
	"min_coverage":      "Min coverage",
	"min_diff_coverage": "Min diff coverage",
	"max_coverage_drop": "Max coverage drop",
}

const gateRangeMsg = " must be a percentage between 0 and 100."

// validGate checks the gate's percentages. A non-empty second result is a
// human-readable validation message; the gate is then zero.
func validGate(gate store.Gate) (store.Gate, string) {
	for _, f := range []struct {
		name  string
		value *float64
	}{
		{"min_coverage", gate.MinCoverage},
		{"min_diff_coverage", gate.MinDiffCoverage},
		{"max_coverage_drop", gate.MaxCoverageDrop},
	} {
		if f.value != nil && (*f.value < 0 || *f.value > 100) {
			return store.Gate{}, gateRuleLabels[f.name] + gateRangeMsg
		}
	}
	return gate, ""
}

// validRetention accepts the retention windows the Defaults selector
// offers: 90 days, 1 year, or 0 (keep forever).
func validRetention(days int) bool {
	switch days {
	case 0, 90, 365:
		return true
	}
	return false
}

// workspaceSettingsDTO is the workspace settings page for the app. The
// upload token rides only as its masked form, and only for owners; the
// value itself comes from reveal-token.
type workspaceSettingsDTO struct {
	Workspace workspaceDTO `json:"workspace"`
	Owner     bool         `json:"owner"`
	RepoCount int          `json:"repo_count"`
	Reporting reportingDTO `json:"reporting"`
	// ServerURL is GOCOV_SERVER for self-hosters; null on the hosted
	// service, where the CLI already defaults to it.
	ServerURL   *string `json:"server_url"`
	TokenMasked *string `json:"token_masked"`
}

type workspaceDTO struct {
	Forge  string `json:"forge"`
	Prefix string `json:"prefix"`
	// DefaultBranch and Gate are what repos registered from now on inherit.
	DefaultBranch string `json:"default_branch"`
	// ReportRetentionDays is 0 for "keep forever".
	ReportRetentionDays int     `json:"report_retention_days"`
	Gate                gateDTO `json:"gate"`
}

type reportingDTO struct {
	// Available is false where this deployment has no connect mechanism
	// for the forge at all.
	Available bool   `json:"available"`
	State     string `json:"state"` // on / off / broken
	Account   string `json:"account"`
	// ConnectURL starts the connect or install flow, "" when unavailable.
	ConnectURL string `json:"connect_url"`
}

// newReportingDTO is the Reporting card as the app reads it, shared by
// the settings endpoint and the setup screen.
func (s *Server) newReportingDTO(r *http.Request, ws *store.Workspace) reportingDTO {
	state, account := reportingState(ws)
	return reportingDTO{
		Available:  s.reportingAvailable(ws),
		State:      state,
		Account:    account,
		ConnectURL: s.connectURL(r, ws),
	}
}

// workspaceSettingsInput is what the app posts to the settings endpoint.
type workspaceSettingsInput struct {
	DefaultBranch       string  `json:"default_branch"`
	ReportRetentionDays int     `json:"report_retention_days"`
	Gate                gateDTO `json:"gate"`
}

// newWorkspaceSettingsDTO copies the workspace out for the app. owner
// decides what it carries: the masked token is an owner's.
func (s *Server) newWorkspaceSettingsDTO(r *http.Request, ws *store.Workspace, owner bool) workspaceSettingsDTO {
	dto := workspaceSettingsDTO{
		Workspace: workspaceDTO{
			Forge:               ws.Forge,
			Prefix:              ws.Prefix,
			DefaultBranch:       ws.DefaultBranch,
			ReportRetentionDays: ws.ReportRetentionDays,
			Gate:                newGateDTO(ws.Gate),
		},
		Owner:     owner,
		Reporting: s.newReportingDTO(r, ws),
	}
	if server := s.baseURL; server != hosted.DefaultServer {
		dto.ServerURL = new(server)
	}
	if owner {
		dto.TokenMasked = new(maskSecret(ws.Token))
	}
	if repos, err := s.workspaceRepos(r, ws); err == nil {
		dto.RepoCount = len(repos)
	}
	return dto
}

// handleAPIWorkspace implements GET /api/ui/workspace-settings/{forge}/{prefix...}.
func (s *Server) handleAPIWorkspace(w http.ResponseWriter, r *http.Request) {
	ws, role := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	s.writeJSON(w, s.newWorkspaceSettingsDTO(r, ws, role == store.RoleOwner))
}

// handleAPIWorkspaceSettings implements
// POST /api/ui/workspace-settings/save/{forge}/{prefix...}.
func (s *Server) handleAPIWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	ws := s.ownerWorkspace(w, r)
	if ws == nil {
		return
	}
	var in workspaceSettingsInput
	if !readJSON(w, r, &in) {
		return
	}
	branch := strings.TrimSpace(in.DefaultBranch)
	if branch == "" {
		invalid(w, "Default branch cannot be empty.")
		return
	}
	gate, errLabel := validGate(in.Gate.gate())
	if errLabel != "" {
		invalid(w, errLabel)
		return
	}
	if !validRetention(in.ReportRetentionDays) {
		invalid(w, "Report retention must be 90 days, 1 year or forever.")
		return
	}
	ws.DefaultBranch = branch
	ws.Gate = gate
	ws.ReportRetentionDays = in.ReportRetentionDays
	if err := s.store.UpdateWorkspace(r.Context(), ws); err != nil {
		s.internalError(w, "updating workspace", err)
		return
	}
	s.writeJSON(w, s.newWorkspaceSettingsDTO(r, ws, true))
}

// tokenRevealDTO hands the upload token itself to an owner who asked for
// it — the one response in the UI API that carries a secret.
type tokenRevealDTO struct {
	Token string `json:"token"`
}

// handleAPIWorkspaceRotate implements
// POST /api/ui/workspace-settings/rotate-token/{forge}/{prefix...}. The new token is
// in the response; the old one is already dead by then.
func (s *Server) handleAPIWorkspaceRotate(w http.ResponseWriter, r *http.Request) {
	ws := s.ownerWorkspace(w, r)
	if ws == nil {
		return
	}
	token := core.NewToken()
	ws.Token = token
	if err := s.store.UpdateWorkspace(r.Context(), ws); err != nil {
		s.internalError(w, "rotating workspace token", err)
		return
	}
	s.log.Info("workspace token rotated", "prefix", ws.Prefix, "user", currentUser(r).DisplayName)
	// writeJSON marks every answer no-store, which a token response needs
	// most of all.
	s.writeJSON(w, tokenRevealDTO{Token: token})
}

// handleAPIWorkspaceReveal implements
// POST /api/ui/workspace-settings/reveal-token/{forge}/{prefix...}: the stored token
// behind the masked form, for an owner who asked to see it. It lives on
// the workspace row in the clear, so this exposes nothing the database
// does not already hold.
func (s *Server) handleAPIWorkspaceReveal(w http.ResponseWriter, r *http.Request) {
	ws := s.ownerWorkspace(w, r)
	if ws == nil {
		return
	}
	s.writeJSON(w, tokenRevealDTO{Token: ws.Token})
}

// handleAPIWorkspaceDisconnect implements
// POST /api/ui/workspace-settings/disconnect/{forge}/{prefix...}.
func (s *Server) handleAPIWorkspaceDisconnect(w http.ResponseWriter, r *http.Request) {
	ws := s.ownerWorkspace(w, r)
	if ws == nil {
		return
	}
	if !s.disconnectWorkspace(w, r, ws) {
		return
	}
	s.writeJSON(w, s.newWorkspaceSettingsDTO(r, ws, true))
}

// handleAPIWorkspaceDelete implements
// POST /api/ui/workspace-settings/delete/{forge}/{prefix...}: the workspace and its
// repos and reports go (the store cascades). Uploads with the token start
// failing at once; nothing changes on the forge.
func (s *Server) handleAPIWorkspaceDelete(w http.ResponseWriter, r *http.Request) {
	ws := s.ownerWorkspace(w, r)
	if ws == nil {
		return
	}
	if err := s.store.DeleteWorkspace(r.Context(), ws.ID); err != nil {
		s.internalError(w, "deleting workspace", err)
		return
	}
	s.log.Info("workspace deleted", "prefix", ws.Prefix, "user", currentUser(r).DisplayName)
	w.WriteHeader(http.StatusNoContent)
}

// workspaceRepos lists the repos under the workspace's prefix.
func (s *Server) workspaceRepos(r *http.Request, ws *store.Workspace) ([]*store.Repo, error) {
	return s.store.ListWorkspaceRepos(r.Context(), ws.Forge, ws.Prefix)
}
