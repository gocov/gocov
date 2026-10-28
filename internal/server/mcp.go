package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gocov/gocov/internal/mcp"
	"github.com/gocov/gocov/internal/readapi"
	"github.com/gocov/gocov/internal/store"
)

// /mcp is the read API as a remote MCP server (Streamable HTTP), for a
// client that is given a URL rather than a command to start — a claude.ai
// connector, say. It offers the get_report tool gocov mcp offers, but has
// no checkout to detect a repo from, so forge and repo are required.
//
// On an open instance it serves every repo to anyone. Behind sign-in its
// client signs in (mcpauth.go) and reads what its user may see: the repos
// of the user's workspaces, and public reports. A repo it may not serve
// answers like one that does not exist, so it tells nobody which private
// repos are tracked. Without a public URL to sign clients in by, it serves
// public reports alone, as the anonymous read API does.

var getReportSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"forge":  map[string]any{"type": "string", "enum": []string{"github", "gitlab", "bitbucket"}, "description": "the forge the repo lives on"},
		"repo":   map[string]any{"type": "string", "description": "repo slug, workspace/repo, e.g. gocov/gocov"},
		"commit": map[string]any{"type": "string", "description": "commit SHA; without it, the newest report on branch"},
		"branch": map[string]any{"type": "string", "description": "the branch to read the newest report on (default: the repo's default branch)"},
		"format": map[string]any{"type": "string", "enum": []string{"text", "json"}, "description": "text (default) or the read API's JSON"},
	},
	"required": []string{"forge", "repo"},
}

type getReportArgs struct {
	Forge  string `json:"forge"`
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Branch string `json:"branch"`
	Format string `json:"format"`
}

func (s *Server) newMCPServer(version string) *mcp.Server {
	return &mcp.Server{Name: "gocov", Version: version, Tools: []mcp.Tool{{
		Name:  "get_report",
		Title: "Coverage report",
		Description: "Read a commit's coverage report from gocov: total coverage, the change against its base, " +
			"and for a pull request the changed lines no test ran. Name the repo by forge and slug, and a commit " +
			"or a branch; with neither it reads the newest report on the default branch. It reads the repos of " +
			"the signed-in user's workspaces and public repos.",
		InputSchema: getReportSchema,
		Call:        s.mcpGetReport,
	}}}
}

// mcpViewerMember reports whether the signed-in MCP client's user belongs
// to a workspace repo is in: the report pages' member rule.
func (s *Server) mcpViewerMember(ctx context.Context, repo *store.Repo) (bool, error) {
	u := currentUserCtx(ctx)
	if u == nil {
		return false, nil
	}
	wss, err := s.store.ListWorkspacesForUser(ctx, u.ID)
	if err != nil {
		return false, err
	}
	return s.scopeFor(wss).allows(repo), nil
}

func (s *Server) mcpGetReport(ctx context.Context, raw json.RawMessage) (string, error) {
	var a getReportArgs
	if raw != nil {
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
	}
	a.Forge, a.Repo = strings.TrimSpace(a.Forge), strings.Trim(strings.TrimSpace(a.Repo), "/")
	if a.Forge == "" || a.Repo == "" {
		return "", errors.New("name the repo: forge (github, gitlab or bitbucket) and repo (workspace/repo)")
	}
	hidden := fmt.Errorf("no public report for %s/%s: the repo is not tracked on this server, or its reports are not public", a.Forge, a.Repo)
	if currentUserCtx(ctx) != nil {
		hidden = fmt.Errorf("no report you may read for %s/%s: the repo is not tracked on this server, or it is not in one of your workspaces and its reports are not public", a.Forge, a.Repo)
	}
	repo, err := s.store.RepoBySlug(ctx, a.Forge, a.Repo)
	if errors.Is(err, store.ErrNotFound) {
		return "", hidden
	}
	if err != nil {
		s.log.Error("mcp: loading repo", "err", err)
		return "", errors.New("internal error")
	}
	if s.authEnabled() {
		member, err := s.mcpViewerMember(ctx, repo)
		if err != nil {
			s.log.Error("mcp: checking access", "err", err)
			return "", errors.New("internal error")
		}
		if !member && !s.servesPublicly(repo) {
			return "", hidden
		}
	}
	rep, err := s.loadReport(ctx, repo, a.Commit, a.Branch)
	var none noReportError
	if errors.As(err, &none) {
		return "", fmt.Errorf("%s/%s: %s; the commit's CI must upload its coverage first", a.Forge, a.Repo, none)
	}
	if err != nil {
		s.log.Error("mcp: loading commit report", "err", err)
		return "", errors.New("internal error")
	}
	if a.Format == "json" {
		b, err := json.Marshal(rep)
		return string(b), err
	}
	var sb bytes.Buffer
	readapi.Print(&sb, rep)
	return sb.String(), nil
}
