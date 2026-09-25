package server

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
)

// The dashboard (GET /) is scoped to one workspace at a time. A workspace is a
// group of repos sharing a slug prefix; a tracked store.Workspace row enriches
// the group with reporting state and a settings page, but an untracked prefix
// (open instances, or repos whose workspace was never registered) still gets a
// group so its repos remain visible. The switcher moves between groups via
// ?ws=<forge>/<prefix>.

const dashStaleAfter = 14 * 24 * time.Hour

// dashboardView is the whole page: the selected workspace, the switcher over
// all groups the viewer can pick, the selected group's repos, and the rollups
// (stats + needs-attention) derived from them.
type dashboardView struct {
	Current   *wsGroup
	Switcher  []*wsGroup
	Repos     []*dashRepo
	Stats     dashStats
	Attention []attnItem
}

// wsGroup is one workspace in the switcher — its identity, a repo count, and a
// weighted-coverage rollup so the picker previews each workspace's health.
type wsGroup struct {
	Prefix    string
	Forge     string
	Workspace *store.Workspace
	ForgeName string // GitHub / Bitbucket / GitLab
	RepoCount int
	HasCov    bool
	Pct       float64
	Current   bool

	repos []*store.Repo // repos bucketed into this group (assembly-only)
}

// dashRepo is one row of the repositories table.
type dashRepo struct {
	Repo   *store.Repo
	Name   string // slug with the workspace prefix trimmed
	Latest *store.CommitReport
	Gate   string // pass / fail / ""
	Stale  bool
	Series []float64 // the sparkline's points, for the app to plot

	HasReport bool
	CovVal    float64
	HasDelta  bool
	DropVal   float64
}

// dashStats is the three-up rollup above the tables.
type dashStats struct {
	HasCoverage    bool
	CoveragePct    float64
	GatesPassing   int
	GatesTotal     int
	StaleCount     int
	ReportingSub   string // "gocov[bot]", the granting account, or ""
	ReportingState string // on / off / broken / "" (styling)
}

// attnItem is one needs-attention notice as data: which condition raised
// it, the repo it is about and the numbers behind it. The app writes the
// sentence.
type attnItem struct {
	cause       string // failing / stale
	name        string // repo name as the table shows it
	repo        *store.Repo
	coverage    *float64
	minCoverage *float64
	staleDays   *int
}

// dashboardDTO is the dashboard as the app reads it: the assembly below,
// minus the sentences.
type dashboardDTO struct {
	// NeedsOnboarding is the state a hosted user with no workspace lands
	// in: the app routes itself to onboarding. The rest is then empty.
	NeedsOnboarding bool           `json:"needs_onboarding"`
	CanOnboard      bool           `json:"can_onboard"`
	Current         *wsGroupDTO    `json:"current"`
	Switcher        []wsGroupDTO   `json:"switcher"`
	Repos           []dashRepoDTO  `json:"repos"`
	Stats           dashStatsDTO   `json:"stats"`
	Attention       []attentionDTO `json:"attention"`
}

// wsGroupDTO is one workspace in the switcher.
type wsGroupDTO struct {
	Forge     string   `json:"forge"`
	Prefix    string   `json:"prefix"`
	ForgeName string   `json:"forge_name"`
	RepoCount int      `json:"repo_count"`
	Coverage  *float64 `json:"coverage"`
	Current   bool     `json:"current"`
	// Tracked marks a registered workspace — the ones with a settings page.
	Tracked bool `json:"tracked"`
}

// dashRepoDTO is one row of the repositories table.
type dashRepoDTO struct {
	Forge      string     `json:"forge"`
	Slug       string     `json:"slug"`
	Name       string     `json:"name"`
	Coverage   *float64   `json:"coverage"`
	Delta      *float64   `json:"delta"`
	Gate       string     `json:"gate"` // pass / fail / none
	Stale      bool       `json:"stale"`
	Series     []float64  `json:"series"`
	UploadedAt *time.Time `json:"uploaded_at"`
}

// dashStatsDTO is the rollup above the tables.
type dashStatsDTO struct {
	Coverage     *float64 `json:"coverage"`
	GatesPassing int      `json:"gates_passing"`
	GatesTotal   int      `json:"gates_total"`
	StaleCount   int      `json:"stale_count"`
	Reporting    string   `json:"reporting"` // connected / not_connected / broken
	ReportingAs  string   `json:"reporting_as"`
}

// attentionDTO is one needs-attention notice as data: which condition
// raised it and the numbers behind it. The app writes the sentence.
type attentionDTO struct {
	Kind        string   `json:"kind"` // failing / stale
	Forge       string   `json:"forge"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Coverage    *float64 `json:"coverage"`
	MinCoverage *float64 `json:"min_coverage"`
	StaleDays   *int     `json:"stale_days"`
}

// reportingStates maps the styling state the dashboard computes to the
// word the API reports it with.
var reportingStates = map[string]string{"on": "connected", "off": "not_connected", "broken": "broken"}

// handleAPIDashboard implements GET /api/ui/dashboard?ws=.
func (s *Server) handleAPIDashboard(w http.ResponseWriter, r *http.Request) {
	dto := dashboardDTO{
		// A signed-in user may register a workspace; an open instance has
		// no identity to register from.
		CanOnboard: currentUser(r) != nil,
		Switcher:   []wsGroupDTO{},
		Repos:      []dashRepoDTO{},
		Attention:  []attentionDTO{},
	}
	tracked, err := s.viewerWorkspaces(r)
	if err != nil {
		s.internalError(w, "scoping repos", err)
		return
	}
	scope := s.scopeFor(tracked)
	// A hosted user without a single workspace membership would see a
	// permanently empty dashboard; onboarding is the only useful screen
	// for them (M3/R1), and the app routes itself there.
	if s.hosted && scope.scoped && len(scope.prefixes) == 0 && currentUser(r) != nil {
		dto.NeedsOnboarding = true
		s.writeJSON(w, dto)
		return
	}
	dash, err := s.buildDashboard(r, strings.TrimSpace(r.FormValue("ws")), scope, tracked)
	if err != nil {
		s.internalError(w, "building dashboard", err)
		return
	}
	if dash == nil { // no repos and no workspaces at all
		s.writeJSON(w, dto)
		return
	}

	for _, g := range dash.Switcher {
		dto.Switcher = append(dto.Switcher, newWSGroupDTO(g))
	}
	if dash.Current != nil {
		cur := newWSGroupDTO(dash.Current)
		dto.Current = &cur
	}
	for _, row := range dash.Repos {
		repo := dashRepoDTO{
			Forge:    row.Repo.Forge,
			Slug:     row.Repo.Slug,
			Name:     row.Name,
			Coverage: optPct(row.HasReport, row.CovVal),
			Delta:    optPct(row.HasDelta, row.DropVal),
			Gate:     cmp.Or(row.Gate, "none"),
			Stale:    row.Stale,
			Series:   row.Series,
		}
		if repo.Series == nil {
			repo.Series = []float64{}
		}
		if row.Latest != nil {
			repo.UploadedAt = &row.Latest.CreatedAt
		}
		dto.Repos = append(dto.Repos, repo)
	}
	for _, item := range dash.Attention {
		dto.Attention = append(dto.Attention, attentionDTO{
			Kind:        item.cause,
			Forge:       item.repo.Forge,
			Slug:        item.repo.Slug,
			Name:        item.name,
			Coverage:    item.coverage,
			MinCoverage: item.minCoverage,
			StaleDays:   item.staleDays,
		})
	}
	dto.Stats = dashStatsDTO{
		Coverage:     optPct(dash.Stats.HasCoverage, dash.Stats.CoveragePct),
		GatesPassing: dash.Stats.GatesPassing,
		GatesTotal:   dash.Stats.GatesTotal,
		StaleCount:   dash.Stats.StaleCount,
		Reporting:    reportingStates[dash.Stats.ReportingState],
		ReportingAs:  dash.Stats.ReportingSub,
	}
	s.writeJSON(w, dto)
}

func newWSGroupDTO(g *wsGroup) wsGroupDTO {
	return wsGroupDTO{
		Forge:     g.Forge,
		Prefix:    g.Prefix,
		ForgeName: g.ForgeName,
		RepoCount: g.RepoCount,
		Coverage:  optPct(g.HasCov, g.Pct),
		Current:   g.Current,
		Tracked:   g.Workspace != nil,
	}
}

// buildDashboard assembles the dashboard for the given ?ws selection. Returns a
// nil view when the viewer has no repos and no tracked workspaces at all (the
// template then shows the empty state). tracked is viewerWorkspaces and scope
// is derived from it, both resolved once by the handler.
func (s *Server) buildDashboard(r *http.Request, selected string, scope repoScope, tracked []*store.Workspace) (*dashboardView, error) {
	ctx := r.Context()
	repos, err := s.store.ListRepos(ctx)
	if err != nil {
		return nil, err
	}

	// Seed groups from the viewer's tracked workspaces so a workspace with no
	// uploads yet still appears in the switcher; then bucket every visible repo,
	// creating an untracked group for any prefix that has no workspace row.
	// Groups are keyed by wsKey: the GitHub org and the GitLab group of
	// one name are two workspaces, and the switcher lists both.
	groups := map[wsKey]*wsGroup{}
	order := []wsKey{}
	add := func(prefix, forge string, ws *store.Workspace) *wsGroup {
		key := wsKey{forge, prefix}
		g := groups[key]
		if g == nil {
			g = &wsGroup{Prefix: prefix, Forge: forge, Workspace: ws}
			groups[key] = g
			order = append(order, key)
		}
		if ws != nil && g.Workspace == nil {
			g.Workspace = ws
		}
		return g
	}
	for _, ws := range tracked {
		add(ws.Prefix, ws.Forge, ws)
	}
	for _, repo := range repos {
		if !scope.allows(repo) {
			continue
		}
		g := add(s.groupPrefix(repo, tracked), repo.Forge, nil)
		g.repos = append(g.repos, repo)
	}
	if len(order) == 0 {
		return nil, nil
	}
	slices.SortFunc(order, func(a, b wsKey) int {
		return cmp.Or(cmp.Compare(a.forge, b.forge), cmp.Compare(a.prefix, b.prefix))
	})

	// Resolve the selected group (?ws=forge/prefix); fall back to the
	// first when ?ws is missing or names a group the viewer cannot see.
	forge, prefix, _ := strings.Cut(selected, "/")
	cur := groups[wsKey{forge, prefix}]
	if cur == nil {
		cur = groups[order[0]]
	}

	dv := &dashboardView{Current: cur}
	for _, k := range order {
		g := groups[k]
		s.fillGroupMeta(ctx, g)
		g.Current = g == cur
		dv.Switcher = append(dv.Switcher, g)
	}

	s.fillCurrent(r, dv)
	return dv, nil
}

// viewerWorkspaces lists the workspaces the switcher may offer: the signed-in
// user's memberships, or every tracked workspace on an open instance.
func (s *Server) viewerWorkspaces(r *http.Request) ([]*store.Workspace, error) {
	if u := currentUser(r); u != nil {
		return s.store.ListWorkspacesForUser(r.Context(), u.ID)
	}
	if !s.authEnabled() {
		return s.store.ListWorkspaces(r.Context())
	}
	return nil, nil
}

// groupPrefix resolves the switcher group a repo belongs to: its most specific
// tracked workspace prefix, or the leading slug segment when none is tracked.
func (s *Server) groupPrefix(repo *store.Repo, tracked []*store.Workspace) string {
	if ws := owningWorkspace(repo, tracked); ws != nil {
		return ws.Prefix
	}
	if i := strings.IndexByte(repo.Slug, '/'); i >= 0 {
		return repo.Slug[:i]
	}
	return repo.Slug
}

// fillGroupMeta computes a group's switcher preview: repo count and weighted
// coverage over its repos' latest default-branch reports. This runs for every
// group, so it stays to one report lookup per repo.
func (s *Server) fillGroupMeta(ctx context.Context, g *wsGroup) {
	g.ForgeName = providerLabels[g.Forge]
	g.RepoCount = len(g.repos)
	var covered, total int64
	for _, repo := range g.repos {
		latest, err := s.store.LatestCommitReport(ctx, repo.ID, repo.DefaultBranch)
		if err != nil || latest == nil {
			continue
		}
		covered += latest.CoveredStmts
		total += latest.TotalStmts
	}
	if total > 0 {
		g.HasCov = true
		g.Pct = profile.Percent(covered, total)
	}
}

// fillCurrent builds the selected group's repo rows, the stat rollup and the
// needs-attention list. Repos in the selected group get the full treatment
// (delta + sparkline), reading each branch's recent reports once.
func (s *Server) fillCurrent(r *http.Request, dv *dashboardView) {
	ctx := r.Context()
	cur := dv.Current
	var covered, total int64
	for _, repo := range cur.repos {
		reports, err := s.store.ListBranchCommitReports(ctx, repo.ID, repo.DefaultBranch, trendReportLimit)
		if err != nil {
			continue
		}
		row := &dashRepo{
			Repo: repo,
			Name: strings.TrimPrefix(repo.Slug, cur.Prefix+"/"),
		}
		var latest *store.CommitReport
		if len(reports) > 0 {
			latest = reports[0]
			row.Latest = latest
			row.HasReport = true
			row.CovVal = latest.TotalPct
			covered += latest.CoveredStmts
			total += latest.TotalStmts
		}
		stale := latest != nil && time.Since(latest.CreatedAt) > dashStaleAfter
		row.Stale = stale
		if repo.Gate.Configured() && latest != nil {
			row.Gate = "pass"
			if latest.GateFailed {
				row.Gate = "fail"
			}
		}
		if _, base := core.ReportBaseline(reports); base != nil {
			row.HasDelta = true
			row.DropVal = latest.TotalPct - base.TotalPct
		}
		row.Series = sparkSeries(reports)

		dv.Repos = append(dv.Repos, row)
		s.collectAttention(dv, repo, row, latest, stale)
	}

	// Default order: lowest coverage first (repos without a report sort last),
	// so the rows needing work lead. The client re-sorts on the sort control.
	slices.SortStableFunc(dv.Repos, func(a, b *dashRepo) int {
		if (a.Latest == nil) != (b.Latest == nil) {
			if a.Latest != nil {
				return -1
			}
			return 1
		}
		if a.Latest == nil {
			return cmp.Compare(a.Name, b.Name)
		}
		return cmp.Compare(a.Latest.TotalPct, b.Latest.TotalPct)
	})

	// Attention reads most-severe first: failing, then stale, then no-gate.
	slices.SortStableFunc(dv.Attention, func(a, b attnItem) int {
		return cmp.Compare(attnRank(a.cause), attnRank(b.cause))
	})

	dv.Stats = dashStats{}
	for _, row := range dv.Repos {
		if row.Stale {
			dv.Stats.StaleCount++
		}
	}
	if total > 0 {
		dv.Stats.HasCoverage = true
		dv.Stats.CoveragePct = profile.Percent(covered, total)
	}
	for _, row := range dv.Repos {
		if row.Gate == "" {
			continue
		}
		dv.Stats.GatesTotal++
		if row.Gate == "pass" {
			dv.Stats.GatesPassing++
		}
	}
	s.fillReporting(&dv.Stats, cur)
}

// collectAttention appends the needs-attention entries a repo warrants: a
// failing gate or a stale feed — things that happened. A repo without a gate
// is a standing choice, not an event: listing it made the section permanent,
// and a notice that is always there stops being read. The table still offers
// "Set a gate" on its row and counts it under the No gate filter.
func (s *Server) collectAttention(dv *dashboardView, repo *store.Repo, row *dashRepo, latest *store.CommitReport, stale bool) {
	if row.Gate == "fail" {
		dv.Attention = append(dv.Attention, attnItem{
			cause: "failing", name: row.Name, repo: repo,
			coverage: &latest.TotalPct, minCoverage: repo.Gate.MinCoverage,
		})
	}
	if stale {
		days := int(time.Since(latest.CreatedAt).Hours() / 24)
		dv.Attention = append(dv.Attention, attnItem{
			cause: "stale", name: row.Name, repo: repo,
			coverage: &latest.TotalPct, staleDays: &days,
		})
	}
}

// fillReporting sets the Reporting stat from the current group's tracked
// workspace connection. Untracked groups (or deployments without a one-click
// mechanism) read as not connected, and only a working connection names
// who it posts as.
func (s *Server) fillReporting(st *dashStats, g *wsGroup) {
	st.ReportingState = "off"
	if g.Workspace == nil {
		return
	}
	state, account := reportingState(g.Workspace)
	st.ReportingState = state
	if state == "on" {
		// The GitHub App has no granting account; it posts as its bot.
		st.ReportingSub = cmp.Or(account, "gocov[bot]")
	}
}

func attnRank(cause string) int {
	if cause == "failing" {
		return 0
	}
	return 1
}

// sparkSeries is a repo's recent branch coverage, oldest first: the
// points the dashboard plots as a sparkline and hands the app to draw its
// own. PR reports are excluded — the series follows the branch's own
// commits — and only the last dozen points are kept, since older ones
// crowd the glyph.
func sparkSeries(reports []*store.CommitReport) []float64 {
	var series []float64
	for _, report := range slices.Backward(reports) {
		if report.PRID == "" {
			series = append(series, report.TotalPct)
		}
	}
	if len(series) > 12 {
		series = series[len(series)-12:]
	}
	return series
}
