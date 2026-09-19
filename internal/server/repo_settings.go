package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/ignore"
	"github.com/gocov/gocov/internal/store"
)

// Repo settings page — the per-repository counterpart to workspace settings:
// coverage gates, base branch, the upload token and repo removal. Like every
// other tenant surface it is members-only; a non-member 404s so the repo's
// existence is never revealed. With auth off there are no members, so the
// pages do not exist. Within the workspace the same split as the
// workspace page applies: members read, owners change.

// memberRepo resolves the {forge}/{slug...} path segments to a repo whose
// owning workspace the signed-in user belongs to, writing a 404
// otherwise. It returns the repo, that workspace (for back-links and
// crumbs) and the user's role there.
func (s *Server) memberRepo(w http.ResponseWriter, r *http.Request) (*store.Repo, *store.Workspace, store.Role) {
	u := s.signedIn(w, r)
	if u == nil {
		return nil, nil, ""
	}
	repo, err := s.store.RepoBySlug(r.Context(), r.PathValue("forge"), r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) {
		tenantNotFound(w, r)
		return nil, nil, ""
	}
	if err != nil {
		s.internalError(w, "loading repo", err)
		return nil, nil, ""
	}
	memberOf, err := s.store.ListWorkspacesForUser(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, "listing memberships", err)
		return nil, nil, ""
	}
	ws := owningWorkspace(repo, memberOf)
	if ws == nil {
		tenantNotFound(w, r)
		return nil, nil, ""
	}
	role, _, err := s.seat(r.Context(), u, ws)
	if err != nil {
		s.internalError(w, "listing memberships", err)
		return nil, nil, ""
	}
	return repo, ws, role
}

// ownerRepo is memberRepo for the owner-only routes: a member who is not
// an owner gets a 403 instead.
func (s *Server) ownerRepo(w http.ResponseWriter, r *http.Request) (*store.Repo, *store.Workspace) {
	repo, ws, role := s.memberRepo(w, r)
	if repo == nil {
		return nil, nil
	}
	if role != store.RoleOwner {
		ownersOnly(w, r)
		return nil, nil
	}
	return repo, ws
}

// badgeMarkdown is the copy-paste snippet the repo page and repo settings
// both hand out — one definition, so the two copy buttons can never drift.
// The badge links to the repo page: for a public repo that is a report any
// README reader can open, for a private one the login wall answers as it
// always did.
func (s *Server) badgeMarkdown(repo *store.Repo) string {
	base := strings.TrimSuffix(s.baseURL, "/")
	return fmt.Sprintf("[![coverage](%s%s)](%s%s?ref=badge)", base, badgeURL(repo), base, repoURL(repo))
}

// publicReportsSwitch reports whether the repo-settings "Public reports"
// switch is meaningful for this repo — a repo the forge reports public, on
// an instance that allows public reports. Render and save both consult it,
// so a save can never flip a value the form did not show.
func (s *Server) publicReportsSwitch(repo *store.Repo) bool {
	return s.publicReports && repo.Visibility == store.VisibilityPublic
}

// repoSettingsDTO is the repo settings page for the app. Like the
// workspace's, the upload token rides only as its masked form and only
// for owners.
type repoSettingsDTO struct {
	Repo      repoSettingsHeadDTO `json:"repo"`
	Workspace workspaceRefDTO     `json:"workspace"`
	Owner     bool                `json:"owner"`
	// ShowPublicReports is whether the switch is meaningful at all: a repo
	// the forge reports public, on an instance that allows public reports.
	ShowPublicReports bool    `json:"show_public_reports"`
	TokenMasked       *string `json:"token_masked"`
}

type repoSettingsHeadDTO struct {
	repoRefDTO
	DefaultBranch string  `json:"default_branch"`
	Gate          gateDTO `json:"gate"`
	// IgnorePaths is one pattern per line, as the textarea holds them.
	IgnorePaths   string `json:"ignore_paths"`
	PublicReports bool   `json:"public_reports"`
	BadgeURL      string `json:"badge_url"`
	BadgeMarkdown string `json:"badge_markdown"`
}

type workspaceRefDTO struct {
	Forge  string `json:"forge"`
	Prefix string `json:"prefix"`
}

// repoSettingsInput is what the app posts to the save endpoint.
type repoSettingsInput struct {
	DefaultBranch string  `json:"default_branch"`
	Gate          gateDTO `json:"gate"`
	IgnorePaths   string  `json:"ignore_paths"`
	PublicReports bool    `json:"public_reports"`
}

func (s *Server) newRepoSettingsDTO(repo *store.Repo, ws *store.Workspace, owner bool) repoSettingsDTO {
	dto := repoSettingsDTO{
		Repo: repoSettingsHeadDTO{
			repoRefDTO:    newRepoRefDTO(repo),
			DefaultBranch: repo.DefaultBranch,
			Gate:          newGateDTO(repo.Gate),
			IgnorePaths:   strings.Join(repo.IgnorePaths, "\n"),
			PublicReports: !repo.PublicReportsDisabled,
			BadgeURL:      badgeURL(repo),
			BadgeMarkdown: s.badgeMarkdown(repo),
		},
		Workspace:         workspaceRefDTO{Forge: ws.Forge, Prefix: ws.Prefix},
		Owner:             owner,
		ShowPublicReports: s.publicReportsSwitch(repo),
	}
	if owner {
		dto.TokenMasked = new(maskSecret(repo.Token))
	}
	return dto
}

// handleAPIRepoSettings implements GET /api/ui/repo-settings/{forge}/{slug...}.
func (s *Server) handleAPIRepoSettings(w http.ResponseWriter, r *http.Request) {
	repo, ws, role := s.memberRepo(w, r)
	if repo == nil {
		return
	}
	s.writeJSON(w, s.newRepoSettingsDTO(repo, ws, role == store.RoleOwner))
}

// handleAPIRepoSettingsSave implements
// POST /api/ui/repo-settings/save/{forge}/{slug...}.
func (s *Server) handleAPIRepoSettingsSave(w http.ResponseWriter, r *http.Request) {
	repo, ws := s.ownerRepo(w, r)
	if repo == nil {
		return
	}
	var in repoSettingsInput
	if !readJSON(w, r, &in) {
		return
	}
	branch := strings.TrimSpace(in.DefaultBranch)
	if branch == "" {
		invalid(w, "Base branch cannot be empty.")
		return
	}
	gate, errLabel := validGate(in.Gate.gate())
	if errLabel != "" {
		invalid(w, errLabel)
		return
	}
	ignorePaths := ignore.Parse(in.IgnorePaths)
	if err := ignore.Validate(ignorePaths); err != nil {
		invalid(w, "Ignored files not saved: "+err.Error()+".")
		return
	}
	repo.DefaultBranch = branch
	repo.Gate = gate
	repo.IgnorePaths = ignorePaths
	// The switch may only move where it is meaningful; a private repo's
	// save must not flip the stored value.
	if s.publicReportsSwitch(repo) {
		repo.PublicReportsDisabled = !in.PublicReports
	}
	if err := s.store.UpdateRepo(r.Context(), repo); err != nil {
		s.internalError(w, "updating repo", err)
		return
	}
	s.writeJSON(w, s.newRepoSettingsDTO(repo, ws, true))
}

// handleAPIRepoRotateToken implements
// POST /api/ui/repo-settings/rotate-token/{forge}/{slug...}. The new token
// is in the response; the old one is dead by then.
func (s *Server) handleAPIRepoRotateToken(w http.ResponseWriter, r *http.Request) {
	repo, _ := s.ownerRepo(w, r)
	if repo == nil {
		return
	}
	token, err := core.NewToken()
	if err != nil {
		s.internalError(w, "generating repo token", err)
		return
	}
	repo.Token = token
	if err := s.store.UpdateRepo(r.Context(), repo); err != nil {
		s.internalError(w, "rotating repo token", err)
		return
	}
	s.log.Info("repo token rotated", "slug", repo.Slug, "user", currentUser(r).DisplayName)
	s.writeJSON(w, tokenRevealDTO{Token: token})
}

// handleAPIRepoRevealToken implements
// POST /api/ui/repo-settings/reveal-token/{forge}/{slug...}.
func (s *Server) handleAPIRepoRevealToken(w http.ResponseWriter, r *http.Request) {
	repo, _ := s.ownerRepo(w, r)
	if repo == nil {
		return
	}
	s.writeJSON(w, tokenRevealDTO{Token: repo.Token})
}

// handleAPIRepoDelete implements
// POST /api/ui/repo-settings/delete/{forge}/{slug...}: the repo and its
// uploads and reports go (the store cascades).
func (s *Server) handleAPIRepoDelete(w http.ResponseWriter, r *http.Request) {
	repo, _ := s.ownerRepo(w, r)
	if repo == nil {
		return
	}
	if err := s.store.DeleteRepo(r.Context(), repo.ID); err != nil {
		s.internalError(w, "deleting repo", err)
		return
	}
	s.log.Info("repo deleted", "slug", repo.Slug, "user", currentUser(r).DisplayName)
	w.WriteHeader(http.StatusNoContent)
}
