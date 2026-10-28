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
// It serves what the anonymous read API serves: public reports, or every
// repo on an open instance. It takes no credential yet. A repo it may not
// serve answers like one that does not exist, so it tells nobody which
// private repos are tracked.

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
			"or a branch; with neither it reads the newest report on the default branch. Public repos only.",
		InputSchema: getReportSchema,
		Call:        s.mcpGetReport,
	}}}
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
	repo, err := s.store.RepoBySlug(ctx, a.Forge, a.Repo)
	if errors.Is(err, store.ErrNotFound) {
		return "", hidden
	}
	if err != nil {
		s.log.Error("mcp: loading repo", "err", err)
		return "", errors.New("internal error")
	}
	if s.authEnabled() && !s.servesPublicly(repo) {
		return "", hidden
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
