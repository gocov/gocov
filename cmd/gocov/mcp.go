package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/gocov/gocov/internal/config"
	"github.com/gocov/gocov/internal/mcp"
	"github.com/gocov/gocov/internal/readapi"
)

// gocov mcp serves the read API to an MCP client (an editor or a desktop
// agent) over stdio, for clients that have no shell to run gocov report
// in. It offers one tool, get_report, which answers what gocov report
// prints (internal/mcp has the protocol). Detection runs in the directory
// the client starts it in, so in a checkout it needs no arguments at all.

// getReportSchema is the tool's arguments. Every one is optional: what is
// left out is detected, as gocov report does.
var getReportSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"forge":  map[string]any{"type": "string", "enum": []string{"github", "gitlab", "bitbucket"}, "description": "the forge the repo lives on (default: detected)"},
		"repo":   map[string]any{"type": "string", "description": "repo slug, workspace/repo (default: detected)"},
		"commit": map[string]any{"type": "string", "description": "commit SHA (default: the checked-out commit)"},
		"branch": map[string]any{"type": "string", "description": "read the newest report on this branch instead of one commit"},
		"format": map[string]any{"type": "string", "enum": []string{"text", "json"}, "description": "text (default) or the read API's JSON"},
	},
}

type getReportArgs struct {
	Forge  string `json:"forge"`
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Branch string `json:"branch"`
	Format string `json:"format"`
}

func runMCP(args []string, stdin io.Reader, stdout io.Writer, env envFunc, git gitFunc, doer httpDoer) error {
	cfg, err := config.LoadCLI()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	server := fs.String("server", cmp.Or(cfg.Server, defaultServer), "gocov server URL (or $GOCOV_SERVER)")
	token := fs.String("token", cfg.Token, "upload token, for a private repo (or $GOCOV_TOKEN)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: gocov mcp [flags]")
	}
	getReport := func(_ context.Context, raw json.RawMessage) (string, error) {
		var a getReportArgs
		if raw != nil {
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
		}
		body, err := fetchReport(reportQuery{
			server: *server, forge: a.Forge, repo: a.Repo, commit: a.Commit, branch: a.Branch, token: *token,
		}, env, git, doer)
		if err != nil || a.Format == "json" {
			return string(body), err
		}
		var rep readapi.Report
		if err := json.Unmarshal(body, &rep); err != nil {
			return "", fmt.Errorf("decoding report: %w", err)
		}
		var sb bytes.Buffer
		readapi.Print(&sb, &rep)
		return sb.String(), nil
	}
	s := &mcp.Server{Name: "gocov", Version: version, Tools: []mcp.Tool{{
		Name:  "get_report",
		Title: "Coverage report",
		Description: "Read a commit's coverage report from gocov: total coverage, the change against its base, " +
			"and for a pull request the changed lines no test ran. Without arguments it reads the checked-out " +
			"commit of the repo the server runs in; the commit's CI must have uploaded its coverage first.",
		InputSchema: getReportSchema,
		Call:        getReport,
	}}}
	return s.ServeStdio(context.Background(), stdin, stdout)
}
