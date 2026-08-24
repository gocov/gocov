// Answering "which repo is this upload for, and may this token write to
// it?" — bearer token lookup, the repo the request names, and the
// automatic registration a workspace token performs for a repo seen for
// the first time.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/store"
)

// lookupUploadToken authenticates the Bearer token as either a per-repo
// token or a workspace token, writing the error response itself. Runs
// before the request body is parsed.
func (s *Server) lookupUploadToken(w http.ResponseWriter, r *http.Request, token string) (*store.Repo, *store.Workspace, bool) {
	ctx := r.Context()
	repo, err := s.store.RepoByToken(ctx, token)
	if err == nil {
		return repo, nil, true
	}
	if !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, "looking up token", err)
		return nil, nil, false
	}
	ws, err := s.store.WorkspaceByToken(ctx, token)
	if err == nil {
		return nil, ws, true
	}
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusUnauthorized, "invalid token")
		return nil, nil, false
	}
	s.internalError(w, "looking up workspace token", err)
	return nil, nil, false
}

// repoNameRe bounds the repo part of auto-registered slugs: one path
// segment, conservative charset, sane length.
var repoNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// maxRepoNameSegments bounds a GitLab repo name's depth below the
// workspace; GitLab itself caps group nesting at 20 levels.
const maxRepoNameSegments = 21

// validRepoName validates the repo part of a workspace-token slug.
// GitLab projects can sit in subgroups below the registered namespace, so
// a gitlab name may span several path segments; other forges take exactly
// one.
func validRepoName(forgeName, name string) bool {
	segments := strings.Split(name, "/")
	if forgeName != "gitlab" && len(segments) != 1 {
		return false
	}
	if len(segments) > maxRepoNameSegments {
		return false
	}
	for _, s := range segments {
		// repoNameRe's charset admits "." and ".." — as path segments they
		// are traversal, not names.
		if s == "." || s == ".." || !repoNameRe.MatchString(s) {
			return false
		}
	}
	return true
}

// resolveUploadRepo maps the authenticated token to the target repo,
// writing the error response itself on failure. Workspace tokens require
// the repo slug, must match the workspace prefix, and register unknown
// repos on the fly.
func (s *Server) resolveUploadRepo(w http.ResponseWriter, r *http.Request, repo *store.Repo, ws *store.Workspace, slug string) (_ *store.Repo, created, ok bool) {
	ctx := r.Context()
	if repo != nil {
		if slug != "" && slug != repo.Slug {
			httpError(w, http.StatusForbidden, "token is for repo %q, not %q", repo.Slug, slug)
			return nil, false, false
		}
		return repo, false, true
	}

	if slug == "" {
		httpError(w, http.StatusBadRequest, "workspace tokens require the repo field")
		return nil, false, false
	}
	name, matched := strings.CutPrefix(slug, ws.Prefix+"/")
	if !matched {
		httpError(w, http.StatusForbidden, "token is for workspace %q, not %q", ws.Prefix, slug)
		return nil, false, false
	}
	if !validRepoName(ws.Forge, name) {
		httpError(w, http.StatusBadRequest, "invalid repo name %q under workspace %q", slug, ws.Prefix)
		return nil, false, false
	}

	repo, err := s.store.RepoBySlug(ctx, slug)
	if err == nil {
		return repo, false, true
	}
	if !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, "looking up repo", err)
		return nil, false, false
	}
	repo, err = s.autoCreateRepo(ctx, ws, slug)
	if errors.Is(err, forge.ErrRepoNotFound) {
		httpError(w, http.StatusNotFound, "repo %q not found on %s", slug, ws.Forge)
		return nil, false, false
	}
	if err != nil {
		s.internalError(w, "auto-registering repo", err)
		return nil, false, false
	}
	return repo, true, true
}

// autoCreateRepo registers a repo first seen through a workspace token.
// The default branch is asked from the forge when the workspace has a
// one-click connection, then falls back to the workspace default and
// finally to "main". A forge that positively says the repo does not
// exist aborts the registration (ErrRepoNotFound), so a leaked workspace
// token cannot fill the dashboard with invented repos.
func (s *Server) autoCreateRepo(ctx context.Context, ws *store.Workspace, slug string) (*store.Repo, error) {
	branch := ""
	fg := s.connectedForge(ctx, ws, ws.Forge)
	if fg != nil {
		b, err := fg.GetDefaultBranch(ctx, slug)
		switch {
		case err == nil && b != "":
			branch = b
		case errors.Is(err, forge.ErrRepoNotFound):
			return nil, err
		case err != nil && !errors.Is(err, forge.ErrNotImplemented):
			// Transient forge trouble must not block a legitimate first
			// upload; fall back to the workspace default branch.
			s.log.Warn("get default branch", "repo", slug, "err", err)
		}
	}
	if branch == "" {
		branch = ws.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}

	token, err := newToken()
	if err != nil {
		return nil, err
	}
	repo := &store.Repo{
		Forge:         ws.Forge,
		Slug:          slug,
		Token:         token,
		DefaultBranch: branch,
		Gate:          ws.Gate,
	}
	if err := s.store.CreateRepo(ctx, repo); err != nil {
		// A concurrent first upload may have won the race; use its repo.
		if existing, lookupErr := s.store.RepoBySlug(ctx, slug); lookupErr == nil {
			return existing, nil
		}
		return nil, err
	}
	s.log.Info("auto-registered repo", "slug", slug, "default_branch", branch, "workspace", ws.Prefix)
	return repo, nil
}

// newToken generates an upload token for auto-registered repos.
func newToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
