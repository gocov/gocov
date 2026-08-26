package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/hosted"
	"github.com/gocov/gocov/internal/store"
)

// workspaceURL builds an in-site link to a workspace page. The prefix is
// escaped into a single path segment because GitLab namespace paths nest
// ("grp/sub" → "grp%2Fsub"); the router decodes it back via PathValue.
func workspaceURL(prefix, suffix string) string {
	return "/workspaces/" + url.PathEscape(prefix) + suffix
}

// Workspace settings page (M3/R3) — the way workspaces are administered:
// token rotation, default branch, one-click forge connection and gate
// defaults. The onboarding/setup page (R4) lives next to it. Both are
// members-only; anyone else 404s (like every other tenant surface, a
// non-member must not learn the workspace exists).

// memberWorkspace resolves the {prefix} path segment to a workspace the
// signed-in user is a member of, writing a 404 otherwise. With auth off
// there are no members, so the pages do not exist.
func (s *Server) memberWorkspace(w http.ResponseWriter, r *http.Request) *store.Workspace {
	if !s.authEnabled() {
		http.NotFound(w, r)
		return nil
	}
	u := currentUser(r)
	if u == nil {
		http.NotFound(w, r)
		return nil
	}
	prefix := r.PathValue("prefix")
	memberOf, err := s.store.ListWorkspacesForUser(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, "listing memberships", err)
		return nil
	}
	for _, ws := range memberOf {
		if ws.Prefix == prefix {
			return ws
		}
	}
	http.NotFound(w, r)
	return nil
}

// settingsData assembles the template payload for the settings page.
// The upload token never leaves the server either: it is only ever shown
// as newToken right after a rotation.
func (s *Server) settingsData(r *http.Request, ws *store.Workspace, newToken, notice, errMsg string) map[string]any {
	data := map[string]any{
		"Workspace":  ws,
		"ForgeLabel": providerLabel(ws.Forge),
		"NewToken":   newToken,
		"Notice":     notice,
		"Error":      errMsg,
		// Uploads card: the token is shown to members on demand (Reveal).
		// It lives on the workspace row in the clear, so this exposes
		// nothing the DB does not already hold; MaskedToken is the default.
		"Token":       ws.Token,
		"MaskedToken": maskSecret(ws.Token),
		// Self-hosters need GOCOV_SERVER in CI; on the public hosted
		// service the CLI already defaults to it, so the row is dropped.
		"ServerURL":  strings.TrimSuffix(s.baseURL, "/"),
		"ShowServer": strings.TrimSuffix(s.baseURL, "/") != hosted.DefaultServer,
		"GateActive": gateActiveCount(ws.Gate),
	}
	if repos, err := s.workspaceRepos(r, ws); err == nil {
		data["RepoCount"] = len(repos)
	}
	s.addGitHubAppData(r, ws, data)
	s.addBitbucketGrantData(ws, data)
	s.addGitLabGrantData(ws, data)
	s.addReportingState(ws, data)
	return data
}

// addReportingState collapses the per-forge connection flags into the
// single state the consolidated Reporting card renders: whether a
// one-click mechanism exists for this deployment ("available"), whether
// it is currently on/off/broken, and the identity posts carry.
func (s *Server) addReportingState(ws *store.Workspace, data map[string]any) {
	available, _ := data["GitHubApp"].(bool)
	if bb, _ := data["BitbucketConnect"].(bool); bb {
		available = true
	}
	if gl, _ := data["GitLabConnect"].(bool); gl {
		available = true
	}
	state, account := "off", ""
	switch ws.Forge {
	case "github":
		switch {
		case ws.GitHubAppBroken:
			state = "broken"
		case ws.GitHubInstallationID != 0:
			state = "on"
		}
	case "bitbucket":
		account = ws.BitbucketGrantAccount
		switch {
		case ws.BitbucketGrantBroken:
			state = "broken"
		case ws.BitbucketGrantAccount != "":
			state = "on"
		}
	case "gitlab":
		account = ws.GitLabGrantAccount
		switch {
		case ws.GitLabGrantBroken:
			state = "broken"
		case ws.GitLabGrantAccount != "":
			state = "on"
		}
	}
	initial := "?"
	if rs := []rune(account); len(rs) > 0 {
		initial = strings.ToUpper(string(rs[0]))
	}
	data["ReportingAvailable"] = available
	data["ReportingState"] = state
	data["ReportingAccount"] = account
	data["ReportingInitial"] = initial
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

// gateActiveCount reports how many of the workspace gate rules are set.
func gateActiveCount(g store.Gate) int {
	n := 0
	for _, p := range []*float64{g.MinCoverage, g.MinDiffCoverage, g.MaxCoverageDrop} {
		if p != nil {
			n++
		}
	}
	return n
}

// addGitHubAppData fills the GitHub App connection state shared by the
// settings and setup pages (One-Click Connect P1). Absent when the
// deployment has no App or the workspace is not on GitHub — the pages
// then render exactly as before.
func (s *Server) addGitHubAppData(r *http.Request, ws *store.Workspace, data map[string]any) {
	if s.forges.GitHubApp == nil || ws.Forge != "github" {
		return
	}
	data["GitHubApp"] = true
	data["GitHubAppConnected"] = ws.GitHubInstallationID != 0
	data["GitHubAppBroken"] = ws.GitHubAppBroken
	data["GitHubInstallURL"] = s.forges.InstallURL(r.Context())
}

// handleWorkspacePage implements GET /workspaces/{prefix}.
func (s *Server) handleWorkspacePage(w http.ResponseWriter, r *http.Request) {
	ws := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	notice := ""
	if r.FormValue("saved") == "1" {
		notice = "Saved."
	}
	if r.FormValue("connected") == "1" {
		switch ws.Forge {
		case "bitbucket":
			notice = "Workspace connected — statuses, PR comments and reports now post as @" +
				ws.BitbucketGrantAccount + "."
		case "gitlab":
			notice = "Workspace connected — statuses and MR comments now post as @" +
				ws.GitLabGrantAccount + "."
		default:
			notice = "GitHub App connected — statuses, PR comments and check runs now post as gocov[bot]."
		}
	}
	s.render(w, r, "workspace.html", s.settingsData(r, ws, "", notice, ""))
}

// handleWorkspaceRotate implements POST /workspaces/{prefix}/rotate-token.
// The response renders the new token once; the old one is already dead by
// then (single UPDATE, no grace period).
func (s *Server) handleWorkspaceRotate(w http.ResponseWriter, r *http.Request) {
	ws := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	token, err := core.NewToken()
	if err != nil {
		s.internalError(w, "generating workspace token", err)
		return
	}
	ws.Token = token
	if err := s.store.UpdateWorkspace(r.Context(), ws); err != nil {
		s.internalError(w, "rotating workspace token", err)
		return
	}
	s.log.Info("workspace token rotated", "prefix", ws.Prefix, "user", currentUser(r).DisplayName)
	s.render(w, r, "workspace.html", s.settingsData(r, ws, token,
		"Token rotated — the previous token no longer works. Update your CI variable.", ""))
}

// handleWorkspaceSettings implements POST /workspaces/{prefix}/settings:
// default branch and gate defaults for repos registered from now on.
func (s *Server) handleWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	ws := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	branch := strings.TrimSpace(r.FormValue("default_branch"))
	if branch == "" {
		s.settingsError(w, r, ws, "Default branch cannot be empty.")
		return
	}
	gate, errLabel := parseGateForm(r)
	if errLabel != "" {
		s.settingsError(w, r, ws, errLabel)
		return
	}
	retention := ws.ReportRetentionDays
	if raw := strings.TrimSpace(r.FormValue("report_retention_days")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || !validRetention(v) {
			s.settingsError(w, r, ws, "Report retention must be 90 days, 1 year or forever.")
			return
		}
		retention = v
	}
	ws.DefaultBranch = branch
	ws.Gate = gate
	ws.ReportRetentionDays = retention
	if err := s.store.UpdateWorkspace(r.Context(), ws); err != nil {
		s.internalError(w, "updating workspace", err)
		return
	}
	http.Redirect(w, r, workspaceURL(ws.Prefix, "?saved=1"), http.StatusSeeOther)
}

// parseGateForm reads the three coverage-gate percentages from a settings
// form (workspace or repo), returning the assembled gate. A non-empty second
// result is a human-readable validation message; the gate is then zero.
func parseGateForm(r *http.Request) (store.Gate, string) {
	gate := store.Gate{}
	for _, f := range []struct {
		name  string
		field **float64
		label string
	}{
		{"min_coverage", &gate.MinCoverage, "Min coverage"},
		{"min_diff_coverage", &gate.MinDiffCoverage, "Min diff coverage"},
		{"max_coverage_drop", &gate.MaxCoverageDrop, "Max coverage drop"},
	} {
		raw := strings.TrimSpace(r.FormValue(f.name))
		if raw == "" {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 || v > 100 {
			return store.Gate{}, f.label + " must be a percentage between 0 and 100."
		}
		*f.field = &v
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

// handleWorkspaceDelete implements POST /workspaces/{prefix}/delete: it
// removes the workspace and cascades its repos and coverage reports (the
// store does the cascade). Members only; uploads with the token start
// failing at once and nothing is changed on the forge.
func (s *Server) handleWorkspaceDelete(w http.ResponseWriter, r *http.Request) {
	ws := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	if err := s.store.DeleteWorkspace(r.Context(), ws.ID); err != nil {
		s.internalError(w, "deleting workspace", err)
		return
	}
	s.log.Info("workspace deleted", "prefix", ws.Prefix, "user", currentUser(r).DisplayName)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// settingsError re-renders the settings page with a validation message.
func (s *Server) settingsError(w http.ResponseWriter, r *http.Request, ws *store.Workspace, msg string) {
	w.WriteHeader(http.StatusBadRequest)
	s.render(w, r, "workspace.html", s.settingsData(r, ws, "", "", msg))
}

// handleWorkspaceSetup implements GET /workspaces/{prefix}/setup (M3/R4):
// the onboarding page with the forge-appropriate CI snippet, the upload
// token pre-filled (D6) and the waiting-for-first-upload state.
func (s *Server) handleWorkspaceSetup(w http.ResponseWriter, r *http.Request) {
	ws := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	data, err := s.setupViewData(r, ws)
	if err != nil {
		s.internalError(w, "listing workspace repos", err)
		return
	}
	repos, _ := data["Repos"].([]*store.Repo)
	// Each stage is its own clean panel. Wire up CI (rail 1) is a single
	// card; "I've added these" advances to First upload (rail 2), which
	// polls while waiting and flips to the done state once a repo lands.
	active := 1
	switch {
	case len(repos) > 0:
		active = 2 // First upload — done
	case r.FormValue("awaiting") == "1":
		active = 2 // First upload — waiting
	}
	data["Active"] = active
	data["Forge"] = ws.Forge
	data["Rail"] = onboardingRail(active, ws.Forge, ws.Prefix, len(repos) > 0)
	s.render(w, r, "onboarding.html", data)
}

// handleWorkspaceSetupStatus implements GET /workspaces/{prefix}/setup/status,
// the htmx poll target that flips the waiting state once the first
// upload has auto-registered a repo.
func (s *Server) handleWorkspaceSetupStatus(w http.ResponseWriter, r *http.Request) {
	ws := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	data, err := s.setupViewData(r, ws)
	if err != nil {
		s.internalError(w, "listing workspace repos", err)
		return
	}
	// The poll only owns the waiting card. Once the first upload lands,
	// reload the whole page so the rail and panel move to the clean
	// First-upload done state instead of stacking it under the CI card.
	if repos, _ := data["Repos"].([]*store.Repo); len(repos) > 0 {
		w.Header().Set("HX-Redirect", workspaceURL(ws.Prefix, "/setup"))
		return
	}
	s.renderPartial(w, "onboarding.html", "setup-status", data)
}

// workspaceRepos lists the repos under the workspace's prefix.
func (s *Server) workspaceRepos(r *http.Request, ws *store.Workspace) ([]*store.Repo, error) {
	repos, err := s.store.ListRepos(r.Context())
	if err != nil {
		return nil, err
	}
	var out []*store.Repo
	for _, repo := range repos {
		if strings.HasPrefix(repo.Slug, ws.Prefix+"/") {
			out = append(out, repo)
		}
	}
	return out, nil
}
