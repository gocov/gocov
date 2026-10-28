package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"

	"github.com/gocov/gocov/internal/config"
)

// gocov mcp serves the read API to an MCP client (an editor or a desktop
// agent) over stdio, for clients that have no shell to run gocov report
// in. It speaks the protocol's newline-delimited JSON-RPC 2.0 and offers
// one tool, get_report, which answers what gocov report prints. Detection
// runs in the directory the client starts it in, so in a checkout it needs
// no arguments at all.

// mcpProtocolVersions are the protocol revisions this server speaks,
// newest first: a client asking for one of them gets it back, any other
// gets the newest.
var mcpProtocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// The JSON-RPC error codes the server answers with.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// getReportTool describes the one tool. Every argument is optional: what
// is left out is detected, as gocov report does.
var getReportTool = map[string]any{
	"name":  "get_report",
	"title": "Coverage report",
	"description": "Read a commit's coverage report from gocov: total coverage, the change against its base, " +
		"and for a pull request the changed lines no test ran. Without arguments it reads the checked-out " +
		"commit of the repo the server runs in; the commit's CI must have uploaded its coverage first.",
	"inputSchema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"forge":  map[string]any{"type": "string", "enum": []string{"github", "gitlab", "bitbucket"}, "description": "the forge the repo lives on (default: detected)"},
			"repo":   map[string]any{"type": "string", "description": "repo slug, workspace/repo (default: detected)"},
			"commit": map[string]any{"type": "string", "description": "commit SHA (default: the checked-out commit)"},
			"branch": map[string]any{"type": "string", "description": "read the newest report on this branch instead of one commit"},
			"format": map[string]any{"type": "string", "enum": []string{"text", "json"}, "description": "text (default) or the read API's JSON"},
		},
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
	s := &mcpServer{server: *server, token: *token, env: env, git: git, doer: doer}

	in := bufio.NewScanner(stdin)
	in.Buffer(make([]byte, 0, 64*1024), maxResponseBytes)
	out := json.NewEncoder(stdout)
	for in.Scan() {
		line := bytes.TrimSpace(in.Bytes())
		if len(line) == 0 {
			continue
		}
		if resp := s.handle(line); resp != nil {
			if err := out.Encode(resp); err != nil {
				return err
			}
		}
	}
	return in.Err()
}

type mcpServer struct {
	server, token string
	env           envFunc
	git           gitFunc
	doer          httpDoer
}

// handle answers one message, or returns nil for a notification, which
// gets no answer.
func (s *mcpServer) handle(line []byte) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{rpcParseError, "parse error: " + err.Error()}}
	}
	if req.ID == nil {
		return nil // a notification, such as notifications/initialized
	}
	resp := &rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if req.JSONRPC != "2.0" || req.Method == "" {
		resp.Error = &rpcError{rpcInvalidRequest, "invalid request"}
		return resp
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := mcpProtocolVersions[0]
		if slices.Contains(mcpProtocolVersions, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "gocov", "version": version},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": []any{getReportTool}}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name != "get_report" {
			resp.Error = &rpcError{rpcInvalidParams, fmt.Sprintf("unknown tool %q", p.Name)}
			return resp
		}
		var a getReportArgs
		if len(p.Arguments) > 0 {
			if err := json.Unmarshal(p.Arguments, &a); err != nil {
				resp.Error = &rpcError{rpcInvalidParams, "invalid arguments: " + err.Error()}
				return resp
			}
		}
		resp.Result = s.getReport(a)
	default:
		resp.Error = &rpcError{rpcMethodNotFound, "method not found: " + req.Method}
	}
	return resp
}

// getReport runs the tool. A report that cannot be read is a tool error,
// not a protocol one, so the agent sees why and can act on it.
func (s *mcpServer) getReport(a getReportArgs) map[string]any {
	body, err := fetchReport(reportQuery{
		server: s.server, forge: a.Forge, repo: a.Repo, commit: a.Commit, branch: a.Branch, token: s.token,
	}, s.env, s.git, s.doer)
	text := string(body)
	if err == nil && a.Format != "json" {
		var rep reportResponse
		if err = json.Unmarshal(body, &rep); err == nil {
			var sb bytes.Buffer
			printReport(&sb, &rep)
			text = sb.String()
		} else {
			err = fmt.Errorf("decoding report: %w", err)
		}
	}
	if err != nil {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}
	}
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
}
