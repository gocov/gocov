// Registering a repo the first time an upload names it. A workspace token
// may upload for any repo under its prefix, so the repo row is created on
// demand — with the rules for what a repo may be called, since a slug from
// an upload is untrusted input.

package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/store"
)

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
func ValidRepoName(forgeName, name string) bool {
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

// RegisterRepo registers a repo first seen through a workspace token. The
// workspace's connection is asked for the repo's default branch; having
// none, or one that cannot answer, is fine.
// The default branch is asked from the forge when the workspace has a
// one-click connection, then falls back to the workspace default and
// finally to "main". A forge that positively says the repo does not
// exist aborts the registration (ErrRepoNotFound), so a leaked workspace
// token cannot fill the dashboard with invented repos.
func (p *Pipeline) RegisterRepo(ctx context.Context, ws *store.Workspace, slug string) (*store.Repo, error) {
	branch := ""
	var fg forge.Forge
	if p.Forges != nil {
		fg = p.Forges.Connected(ctx, ws, ws.Forge)
	}
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
			p.Log.Warn("get default branch", "repo", slug, "err", err)
		}
	}
	if branch == "" {
		branch = ws.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}

	token, err := NewToken()
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
	if err := p.Store.CreateRepo(ctx, repo); err != nil {
		// A concurrent first upload may have won the race; use its repo.
		if existing, lookupErr := p.Store.RepoBySlug(ctx, slug); lookupErr == nil {
			return existing, nil
		}
		return nil, err
	}
	p.Log.Info("auto-registered repo", "slug", slug, "default_branch", branch, "workspace", ws.Prefix)
	return repo, nil
}

// RefreshVisibility re-asks the forge whether the repo is public and
// caches the answer on the repo row — the switch behind anonymous report
// pages, so a repo flipped private on the forge closes its pages by the
// next upload at the latest. Best effort: any failure keeps the last
// known state (private until the forge has ever answered) and never
// disturbs the upload.
func (p *Pipeline) RefreshVisibility(ctx context.Context, fg forge.Forge, repo *store.Repo) {
	if fg == nil {
		return
	}
	v, err := fg.GetRepoVisibility(ctx, repo.Slug)
	if errors.Is(err, forge.ErrNotImplemented) {
		return
	}
	if err != nil {
		p.Log.Warn("get repo visibility", "repo", repo.Slug, "err", err)
		return
	}
	if v == repo.Visibility {
		return
	}
	if err := p.Store.SetRepoVisibility(ctx, repo.ID, v); err != nil {
		p.Log.Error("caching repo visibility", "repo", repo.Slug, "err", err)
		return
	}
	p.Log.Info("repo visibility changed", "repo", repo.Slug, "visibility", v)
	repo.Visibility = v
}

// NewToken generates a token: 24 random bytes in hex. Repo and workspace
// tokens are the same shape, and only their hash is ever compared, so one
// generator serves both.
func NewToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// SlugPrefixes returns every slash-boundary prefix of a repo slug,
// longest first: "a/b/c" → ["a/b", "a"]. GitLab namespaces nest, so a
// repo's workspace can sit at any depth (a registered subgroup path is a
// workspace of its own); Bitbucket and GitHub slugs only ever have the
// single-segment prefix.
func SlugPrefixes(slug string) []string {
	var out []string
	for i := len(slug); ; {
		j := strings.LastIndex(slug[:i], "/")
		if j < 0 {
			return out
		}
		out = append(out, slug[:j])
		i = j
	}
}
