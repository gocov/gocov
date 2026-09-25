package server

import (
	"cmp"
	"context"
	"maps"
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

// dashGroup is one switcher group while the dashboard is assembled: a
// workspace (tracked or not) and the visible repos bucketed into it.
type dashGroup struct {
	key   wsKey
	ws    *store.Workspace // nil for an untracked group
	repos []*store.Repo
}

// dashboardDTO is the dashboard as the app reads it: the selected
// workspace, the switcher over every group the viewer can pick, the
// selected group's repos, and the rollups (stats + needs-attention)
// derived from them — minus the sentences, which are the app's to write.
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
// Gate and staleness counts are not among them: the app counts its rows
// for the filter tabs anyway (repoCounts), and the stat tiles read the same.
type dashStatsDTO struct {
	Coverage    *float64 `json:"coverage"`
	Reporting   string   `json:"reporting"` // connected / not_connected / broken
	ReportingAs string   `json:"reporting_as"`
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

// reportingStates maps a workspace's reporting state to the word the
// dashboard reports it with.
var reportingStates = map[string]string{"on": "connected", "off": "not_connected", "broken": "broken"}

// handleAPIDashboard implements GET /api/ui/dashboard?ws=.
func (s *Server) handleAPIDashboard(w http.ResponseWriter, r *http.Request) {
	dto := &dashboardDTO{
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
	// for them, and the app routes itself there.
	if s.hosted && scope.scoped && len(scope.prefixes) == 0 && currentUser(r) != nil {
		dto.NeedsOnboarding = true
		s.writeJSON(w, dto)
		return
	}
	if err := s.buildDashboard(r, dto, strings.TrimSpace(r.FormValue("ws")), scope, tracked); err != nil {
		s.internalError(w, "building dashboard", err)
		return
	}
	s.writeJSON(w, dto)
}

// buildDashboard fills the dashboard for the given ?ws selection. With no
// repos and no tracked workspaces at all it leaves dto empty, which the
// app shows as the empty state. tracked is viewerWorkspaces and scope is
// derived from it, both resolved once by the handler.
func (s *Server) buildDashboard(r *http.Request, dto *dashboardDTO, selected string, scope repoScope, tracked []*store.Workspace) error {
	ctx := r.Context()
	repos, err := s.visibleRepos(ctx, scope)
	if err != nil {
		return err
	}

	// Seed groups from the viewer's tracked workspaces so a workspace with no
	// uploads yet still appears in the switcher; then bucket every visible repo,
	// creating an untracked group for any prefix that has no workspace row.
	// Groups are keyed by wsKey: the GitHub org and the GitLab group of
	// one name are two workspaces, and the switcher lists both.
	groups := map[wsKey]*dashGroup{}
	for _, ws := range tracked {
		key := wsKey{ws.Forge, ws.Prefix}
		groups[key] = &dashGroup{key: key, ws: ws}
	}
	for _, repo := range repos {
		// visibleRepos already scoped the list; checking again keeps one
		// tenant's repos out of another's dashboard should it ever not.
		if !scope.allows(repo) {
			continue
		}
		key := wsKey{repo.Forge, s.groupPrefix(repo, tracked)}
		g := groups[key]
		if g == nil {
			g = &dashGroup{key: key}
			groups[key] = g
		}
		g.repos = append(g.repos, repo)
	}
	if len(groups) == 0 {
		return nil
	}
	order := slices.SortedFunc(maps.Keys(groups), compareWsKey)

	// Resolve the selected group (?ws=forge/prefix); fall back to the
	// first when ?ws is missing or names a group the viewer cannot see.
	forge, prefix, _ := strings.Cut(selected, "/")
	cur := groups[wsKey{forge, prefix}]
	if cur == nil {
		cur = groups[order[0]]
	}

	// Every group previews its coverage from its repos' latest reports,
	// read for all of them at once.
	var ids []int64
	for _, g := range groups {
		for _, repo := range g.repos {
			ids = append(ids, repo.ID)
		}
	}
	latest, err := s.latestReports(ctx, ids)
	if err != nil {
		// The previews are decoration; the page still works without them.
		s.log.Warn("loading dashboard previews", "err", err)
	}
	for _, k := range order {
		g := groupDTO(groups[k], latest)
		g.Current = groups[k] == cur
		dto.Switcher = append(dto.Switcher, g)
		if g.Current {
			dto.Current = &g
		}
	}
	s.fillCurrent(r, dto, cur)
	return nil
}

// visibleRepos lists the repos the scope admits. A scoped viewer reads only
// their workspaces' repos, not every tenant's; nested GitLab memberships
// can list a project twice, so the union is de-duplicated, then ordered
// like ListRepos.
func (s *Server) visibleRepos(ctx context.Context, scope repoScope) ([]*store.Repo, error) {
	if !scope.scoped {
		return s.store.ListRepos(ctx)
	}
	seen := map[int64]bool{}
	var out []*store.Repo
	for k := range scope.prefixes {
		repos, err := s.store.ListWorkspaceRepos(ctx, k.forge, k.prefix)
		if err != nil {
			return nil, err
		}
		for _, repo := range repos {
			if !seen[repo.ID] {
				seen[repo.ID] = true
				out = append(out, repo)
			}
		}
	}
	slices.SortFunc(out, func(a, b *store.Repo) int {
		return cmp.Or(cmp.Compare(a.Forge, b.Forge), cmp.Compare(a.Slug, b.Slug))
	})
	return out, nil
}

// latestReports returns each repo's newest report on its default branch's
// own history, keyed by repo id; repos without one are absent. Like every
// DefaultBranchReports read it leaves DiffCoverage unloaded.
func (s *Server) latestReports(ctx context.Context, repoIDs []int64) (map[int64]*store.CommitReport, error) {
	reports, err := s.store.DefaultBranchReports(ctx, repoIDs, 1)
	if err != nil {
		return nil, err
	}
	latest := make(map[int64]*store.CommitReport, len(reports))
	for id, rs := range reports {
		latest[id] = rs[0]
	}
	return latest, nil
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
	return ownerOf(repo.Slug)
}

// groupDTO is a group's switcher entry: its identity, a repo count, and a
// weighted-coverage rollup over its repos' latest default-branch reports
// (latest, by repo id), so the picker previews each workspace's health.
func groupDTO(g *dashGroup, latest map[int64]*store.CommitReport) wsGroupDTO {
	var covered, total int64
	for _, repo := range g.repos {
		if cr := latest[repo.ID]; cr != nil {
			covered += cr.CoveredStmts
			total += cr.TotalStmts
		}
	}
	return wsGroupDTO{
		Forge:     g.key.forge,
		Prefix:    g.key.prefix,
		RepoCount: len(g.repos),
		Coverage:  optPct(total > 0, profile.Percent(covered, total)),
		Tracked:   g.ws != nil,
	}
}

// fillCurrent builds the selected group's repo rows, the stat rollup and the
// needs-attention list. Repos in the selected group get the full treatment
// (delta + sparkline), from their default branches' recent reports, read
// for the whole group at once.
func (s *Server) fillCurrent(r *http.Request, dto *dashboardDTO, cur *dashGroup) {
	ids := make([]int64, len(cur.repos))
	for i, repo := range cur.repos {
		ids[i] = repo.ID
	}
	history, err := s.store.DefaultBranchReports(r.Context(), ids, trendReportLimit)
	if err != nil {
		// Without history no row can be drawn, as when each repo's own
		// read used to fail; the stats below still describe the group.
		s.log.Warn("loading dashboard rows", "err", err)
		cur = &dashGroup{key: cur.key, ws: cur.ws}
	}
	var covered, total int64
	for _, repo := range cur.repos {
		reports := history[repo.ID]
		row := dashRepoDTO{
			Forge:  repo.Forge,
			Slug:   repo.Slug,
			Name:   strings.TrimPrefix(repo.Slug, cur.key.prefix+"/"),
			Gate:   "none",
			Series: sparkSeries(reports),
		}
		latest, base := core.ReportBaseline(reports)
		if latest != nil {
			row.Coverage = new(latest.TotalPct)
			row.UploadedAt = new(latest.CreatedAt)
			row.Stale = time.Since(latest.CreatedAt) > dashStaleAfter
			covered += latest.CoveredStmts
			total += latest.TotalStmts
			row.Gate = gateState(store.JudgedGate(latest.Gate, repo.Gate), latest.GateFailed)
		}
		if base != nil {
			row.Delta = new(latest.TotalPct - base.TotalPct)
		}
		dto.Repos = append(dto.Repos, row)
		dto.Attention = append(dto.Attention, attention(repo, row, latest)...)
	}

	// Rows go out in slug order; the app sorts them (compareRepos), lowest
	// coverage first by default.

	// Attention reads most-severe first: failing, then stale, then no-gate.
	slices.SortStableFunc(dto.Attention, func(a, b attentionDTO) int {
		return cmp.Compare(attnRank(a.Kind), attnRank(b.Kind))
	})

	dto.Stats = dashStatsDTO{Coverage: optPct(total > 0, profile.Percent(covered, total))}
	dto.Stats.Reporting, dto.Stats.ReportingAs = dashReporting(cur.ws)
}

// attention returns the needs-attention entries a repo warrants: a failing
// gate or a stale feed — things that happened. A repo without a gate is a
// standing choice, not an event: listing it made the section permanent,
// and a notice that is always there stops being read. The table still
// offers "Set a gate" on its row and counts it under the No gate filter.
func attention(repo *store.Repo, row dashRepoDTO, latest *store.CommitReport) []attentionDTO {
	var out []attentionDTO
	item := func(kind string) attentionDTO {
		return attentionDTO{Kind: kind, Forge: repo.Forge, Slug: repo.Slug, Name: row.Name, Coverage: new(latest.TotalPct)}
	}
	if row.Gate == "fail" {
		a := item("failing")
		// Quote the minimum only when that is the rule that failed, at the
		// threshold it was judged by; a diff-coverage or drop failure
		// leaves it out, and the app says only that the gate failed.
		if g := store.JudgedGate(latest.Gate, repo.Gate); core.MinCoverageFailed(g, latest.TotalPct) {
			a.MinCoverage = g.MinCoverage
		}
		out = append(out, a)
	}
	if row.Stale {
		a := item("stale")
		a.StaleDays = new(int(time.Since(latest.CreatedAt).Hours() / 24))
		out = append(out, a)
	}
	return out
}

// dashReporting is the Reporting stat for the current group's tracked
// workspace connection. Untracked groups (or deployments without a
// one-click mechanism) read as not connected, and only a working
// connection names who it posts as.
func dashReporting(ws *store.Workspace) (state, as string) {
	if ws == nil {
		return reportingStates["off"], ""
	}
	state, account := reportingState(ws)
	if state == "on" {
		// The GitHub App has no granting account; it posts as its bot.
		as = cmp.Or(account, appAccount)
	}
	return reportingStates[state], as
}

func attnRank(cause string) int {
	if cause == "failing" {
		return 0
	}
	return 1
}

// sparkSeries is a repo's recent default-branch coverage, oldest first:
// the points the dashboard plots as a sparkline and hands the app to draw
// its own. The reports are DefaultBranchReports', so no PR build is among
// them; only the last dozen points are kept, since older ones crowd the
// glyph. Never nil: a repo without reports plots an empty series.
func sparkSeries(reports []*store.CommitReport) []float64 {
	series := []float64{}
	for _, report := range slices.Backward(reports) {
		series = append(series, report.TotalPct)
	}
	if len(series) > 12 {
		series = series[len(series)-12:]
	}
	return series
}
