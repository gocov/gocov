package server

import (
	"net/http"
	"strings"

	"github.com/gocov/gocov/internal/hosted"
	"github.com/gocov/gocov/internal/store"
)

// Onboarding: where a signed-in account with nowhere to put coverage
// picks a workspace, and what the setup screen on the dashboard reads
// while it waits for the first report. There is no wizard — the work
// happens in the user's repo and CI, so the app pins a setup checklist
// to the workspace's dashboard instead of walking a rail of pages.
//
// Sign-in already happened before onboarding is reachable, so there is
// no separate "connect account" step: for GitHub the workspace is
// created by installing the app (org, repos and bot identity in one
// approval); for Bitbucket/GitLab it is picked from the sign-in
// membership snapshot. Either way the account lands on that workspace's
// dashboard, which is where the checklist lives.

// splitConnectState parses the connect state cookie value (state|prefix).
func splitConnectState(v string) (state, prefix string) {
	state, prefix, _ = strings.Cut(v, "|")
	// A cookie written before the cutover carried a third field naming
	// where the connect started; there is only one destination now.
	prefix, _, _ = strings.Cut(prefix, "|")
	return state, prefix
}

// oidcReady reports whether uploads from the workspace's repos can
// authenticate with a forge-minted OIDC identity token instead of the
// upload token: the workspace has to be connected to its forge, and the
// connection has to work, since it is what the server verifies the
// token's repository against — a broken grant refuses those uploads.
func oidcReady(ws *store.Workspace) bool {
	switch ws.Forge {
	case "github":
		return ws.GitHubInstallationID != 0 && !ws.GitHubAppBroken
	case "bitbucket":
		return ws.BitbucketGrantAccount != "" && !ws.BitbucketGrantBroken
	case "gitlab":
		return ws.GitLabGrantAccount != "" && !ws.GitLabGrantBroken
	}
	return false
}

// connectionBroken reports a forge connection that exists but no longer
// works, so the wizard can point at the reconnect rather than at Connect.
func connectionBroken(ws *store.Workspace) bool {
	switch ws.Forge {
	case "github":
		return ws.GitHubInstallationID != 0 && ws.GitHubAppBroken
	case "bitbucket":
		return ws.BitbucketGrantAccount != "" && ws.BitbucketGrantBroken
	case "gitlab":
		return ws.GitLabGrantAccount != "" && ws.GitLabGrantBroken
	}
	return false
}

// latestReport returns the newest report among the workspace's repos with
// the repo it belongs to, or nils when none has coverage yet. The page
// and the app's poll both start from it.
func (s *Server) latestReport(r *http.Request, repos []*store.Repo) (*store.Repo, *store.CommitReport) {
	for _, repo := range repos {
		rep, err := s.store.LatestCommitReport(r.Context(), repo.ID, repo.DefaultBranch)
		if err != nil || rep == nil {
			continue
		}
		return repo, rep
	}
	return nil, nil
}

// reportsPostedMsg infers, from the workspace's connect state, whether the
// first upload had a reporting surface to post to — no per-upload log
// needed. Empty means nothing was posted back.
func reportsPostedMsg(ws *store.Workspace) string {
	switch ws.Forge {
	case "github":
		if ws.GitHubInstallationID != 0 {
			return "Commit status posted as gocov[bot]."
		}
	case "bitbucket":
		if ws.BitbucketGrantAccount != "" {
			return "Commit status posted as @" + ws.BitbucketGrantAccount + "."
		}
	case "gitlab":
		if ws.GitLabGrantAccount != "" {
			return "Commit status posted as @" + ws.GitLabGrantAccount + "."
		}
	}
	return ""
}

// The onboarding endpoints of the UI API (/api/ui/) — the app's side of
// the two screens above. The picker and its claim answer where the page
// would refuse with a 404, since the app has no page to fall back to; the
// setup screen is a members-only read like the page it replaces, and it
// carries no token, only its masked form and only for an owner.

// onboardingDTO is the workspace picker: who is signed in, and how a
// workspace is chosen on their forge.
type onboardingDTO struct {
	Forge      string `json:"forge"`
	ForgeLabel string `json:"forge_label"`
	Account    string `json:"account"`
	// Mode is "install" where the page shows the GitHub App prompt and
	// "pick" where it lists the memberships read at sign-in.
	Mode string `json:"mode"`
	// InstallURL is set in install mode only; it points at GitHub.
	InstallURL string             `json:"install_url"`
	Rows       []onboardingRowDTO `json:"rows"`
	// MembershipCount is how many workspaces the forge reported at login,
	// whether or not they can be claimed.
	MembershipCount int `json:"membership_count"`
}

// onboardingRowDTO is one claimable workspace and the control it earns,
// as registerRows resolved it.
type onboardingRowDTO struct {
	Prefix string `json:"prefix"`
	State  string `json:"state"`
}

// handleAPIOnboarding implements GET /api/ui/onboarding: the Workspace
// step as data. The ready state (?ws=) has no API twin — the app routes
// a fresh workspace to its dashboard instead.
func (s *Server) handleAPIOnboarding(w http.ResponseWriter, r *http.Request) {
	u := s.registerUser(w, r)
	if u == nil {
		return
	}
	dto := onboardingDTO{
		Forge:           u.Forge,
		ForgeLabel:      providerLabel(u.Forge),
		Account:         u.DisplayName,
		Mode:            "pick",
		Rows:            []onboardingRowDTO{},
		MembershipCount: len(u.ForgeWorkspaces),
	}
	// GitHub with an App configured creates the workspace by installing the
	// app; the token forges pick from the sign-in snapshot.
	if s.forges.GitHubApp != nil && u.Forge == "github" {
		dto.Mode = "install"
		dto.InstallURL = s.forges.InstallURL(r.Context())
		s.writeJSON(w, dto)
		return
	}
	rows, err := s.registerRows(r, u)
	if err != nil {
		s.internalError(w, "resolving registrable workspaces", err)
		return
	}
	for _, row := range rows {
		dto.Rows = append(dto.Rows, onboardingRowDTO{Prefix: row.Prefix, State: row.State})
	}
	s.writeJSON(w, dto)
}

// setupInfoDTO is what the app needs to write a CI snippet for the
// workspace: the snippet itself is assembled client-side, so this is
// every input it varies on, plus the first-report state it waits on.
type setupInfoDTO struct {
	Workspace setupWorkspaceDTO `json:"workspace"`
	Owner     bool              `json:"owner"`
	// Tokenless: the snippet leads with an OIDC identity token instead of
	// GOCOV_TOKEN, which only a working forge connection allows.
	Tokenless bool `json:"tokenless"`
	// ConnectionBroken names the reconnect as what brings tokenless back.
	ConnectionBroken bool `json:"connection_broken"`
	// BaseURL is the OIDC audience and GOCOV_SERVER; ServerImplicit drops
	// the latter from the snippet on the hosted service, where the CLI
	// already defaults to it.
	BaseURL        string `json:"base_url"`
	ServerImplicit bool   `json:"server_implicit"`
	// GitLabCatalog: the CI/CD Catalog component lives on gitlab.com, so
	// only an instance whose GitLab is gitlab.com can offer it.
	GitLabCatalog bool   `json:"gitlab_catalog"`
	CLIVersion    string `json:"cli_version"`
	// TokenMasked is an owner's; the value itself comes from reveal-token.
	TokenMasked *string        `json:"token_masked"`
	Reporting   reportingDTO   `json:"reporting"`
	Status      setupStatusDTO `json:"status"`
}

// setupWorkspaceDTO names the workspace the snippet is for.
type setupWorkspaceDTO struct {
	workspaceRefDTO
	ForgeLabel string `json:"forge_label"`
}

// setupStatusDTO is the part the app polls: whether anything has been
// registered under the workspace yet, and the first report once one lands.
type setupStatusDTO struct {
	RepoCount   int                  `json:"repo_count"`
	FirstReport *setupFirstReportDTO `json:"first_report"`
	// ReportsPosted is the sentence naming the identity a build status
	// appeared as, "" when nothing was posted back.
	ReportsPosted string `json:"reports_posted"`
}

// setupFirstReportDTO is the payoff: the number that just arrived, and
// the commit it came from (in full — the app shortens it).
type setupFirstReportDTO struct {
	Repo         repoRefDTO `json:"repo"`
	Branch       string     `json:"branch"`
	SHA          string     `json:"sha"`
	Coverage     float64    `json:"coverage"`
	CoveredStmts int64      `json:"covered_stmts"`
	TotalStmts   int64      `json:"total_stmts"`
}

// newSetupStatusDTO assembles the polled half of the setup screen, the
// same reading setupViewData takes.
func (s *Server) newSetupStatusDTO(r *http.Request, ws *store.Workspace) (setupStatusDTO, error) {
	repos, err := s.workspaceRepos(r, ws)
	if err != nil {
		return setupStatusDTO{}, err
	}
	dto := setupStatusDTO{RepoCount: len(repos)}
	if len(repos) == 0 {
		return dto, nil
	}
	dto.ReportsPosted = reportsPostedMsg(ws)
	if repo, rep := s.latestReport(r, repos); rep != nil {
		dto.FirstReport = &setupFirstReportDTO{
			Repo:         newRepoRefDTO(repo),
			Branch:       rep.Branch,
			SHA:          rep.CommitSHA,
			Coverage:     rep.TotalPct,
			CoveredStmts: rep.CoveredStmts,
			TotalStmts:   rep.TotalStmts,
		}
	}
	return dto, nil
}

// handleAPIWorkspaceSetup implements
// GET /api/ui/workspace-setup/{forge}/{prefix...}: members read it, as on
// the page; the masked token is an owner's alone.
func (s *Server) handleAPIWorkspaceSetup(w http.ResponseWriter, r *http.Request) {
	ws, role := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	status, err := s.newSetupStatusDTO(r, ws)
	if err != nil {
		s.internalError(w, "listing workspace repos", err)
		return
	}
	owner := role == store.RoleOwner
	baseURL := strings.TrimSuffix(s.baseURL, "/")
	dto := setupInfoDTO{
		Workspace: setupWorkspaceDTO{
			workspaceRefDTO: workspaceRefDTO{Forge: ws.Forge, Prefix: ws.Prefix},
			ForgeLabel:      providerLabel(ws.Forge),
		},
		Owner:            owner,
		Tokenless:        oidcReady(ws),
		ConnectionBroken: connectionBroken(ws),
		BaseURL:          baseURL,
		ServerImplicit:   baseURL == hosted.DefaultServer,
		GitLabCatalog:    s.gitlabIssuers[gitLabDotComIssuer],
		CLIVersion:       hosted.PinnedCLIVersion,
		Reporting:        s.newReportingDTO(r, ws),
		Status:           status,
	}
	if owner {
		dto.TokenMasked = new(maskSecret(ws.Token))
	}
	s.writeJSON(w, dto)
}

// handleAPIWorkspaceSetupStatus implements
// GET /api/ui/workspace-setup-status/{forge}/{prefix...}, the app's poll
// while it waits for the first report.
func (s *Server) handleAPIWorkspaceSetupStatus(w http.ResponseWriter, r *http.Request) {
	ws, _ := s.memberWorkspace(w, r)
	if ws == nil {
		return
	}
	status, err := s.newSetupStatusDTO(r, ws)
	if err != nil {
		s.internalError(w, "listing workspace repos", err)
		return
	}
	s.writeJSON(w, status)
}
