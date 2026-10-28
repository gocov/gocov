// Package mcp is a small Model Context Protocol server: tools only, over
// either transport the protocol defines. gocov mcp serves it over stdio,
// for a client that starts the CLI. The server's /mcp serves it over
// Streamable HTTP, for a client given a URL. Both offer the same
// get_report tool. It holds no session state: every message is answered on
// its own, which is all a tools-only server needs.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
)

// ProtocolVersions are the protocol revisions this server speaks, newest
// first: a client asking for one of them gets it back, any other gets the
// newest.
var ProtocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// The JSON-RPC error codes the server answers with.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
)

// maxMessageBytes bounds one incoming message. A tool call is a few
// hundred bytes.
const maxMessageBytes = 1 << 20

// Tool is one tool the server offers.
type Tool struct {
	Name        string
	Title       string
	Description string
	// InputSchema is the JSON Schema of the arguments object.
	InputSchema map[string]any
	// Call runs the tool on its arguments: a JSON object, or nil when the
	// client sent none. An error is the tool's own: the client gets it as
	// a failed call the model can read, not as a protocol error.
	Call func(ctx context.Context, args json.RawMessage) (string, error)
}

// Server answers MCP messages for its tools.
type Server struct {
	Name, Version string
	Tools         []Tool
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is one JSON-RPC answer.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is a JSON-RPC error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Handle answers one message, or returns nil for a notification, which
// gets no answer.
func (s *Server) Handle(ctx context.Context, msg []byte) *Response {
	var req request
	if err := json.Unmarshal(msg, &req); err != nil {
		return &Response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &Error{CodeParseError, "parse error: " + err.Error()}}
	}
	if req.ID == nil {
		return nil // a notification, such as notifications/initialized
	}
	resp := &Response{JSONRPC: "2.0", ID: req.ID}
	if req.JSONRPC != "2.0" || req.Method == "" {
		resp.Error = &Error{CodeInvalidRequest, "invalid request"}
		return resp
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := ProtocolVersions[0]
		if slices.Contains(ProtocolVersions, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		tools := make([]any, len(s.Tools))
		for i, t := range s.Tools {
			tools[i] = map[string]any{"name": t.Name, "title": t.Title, "description": t.Description, "inputSchema": t.InputSchema}
		}
		resp.Result = map[string]any{"tools": tools}
	case "tools/call":
		resp.Result, resp.Error = s.call(ctx, req.Params)
	default:
		resp.Error = &Error{CodeMethodNotFound, "method not found: " + req.Method}
	}
	return resp
}

func (s *Server) call(ctx context.Context, params json.RawMessage) (any, *Error) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &Error{CodeInvalidParams, "invalid params: " + err.Error()}
	}
	i := slices.IndexFunc(s.Tools, func(t Tool) bool { return t.Name == p.Name })
	if i < 0 {
		return nil, &Error{CodeInvalidParams, "unknown tool: " + p.Name}
	}
	args := bytes.TrimSpace(p.Arguments)
	if bytes.Equal(args, []byte("null")) {
		args = nil
	}
	if len(args) > 0 && args[0] != '{' {
		return nil, &Error{CodeInvalidParams, "arguments must be an object"}
	}
	text, err := s.Tools[i].Call(ctx, args)
	if err != nil {
		return map[string]any{"content": []any{textContent(err.Error())}, "isError": true}, nil
	}
	return map[string]any{"content": []any{textContent(text)}}, nil
}

func textContent(text string) map[string]any { return map[string]any{"type": "text", "text": text} }

// ServeStdio answers newline-delimited messages from in on out until in
// ends: the stdio transport.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if resp := s.Handle(ctx, line); resp != nil {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// ServeHTTP is the Streamable HTTP transport, without sessions or a
// server-initiated stream: a client POSTs one message and gets its answer
// as a JSON body, or 202 for a notification. GET, which would open that
// stream, is refused with 405, as the transport allows.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !slices.Contains(ProtocolVersions, v) {
		http.Error(w, "unsupported MCP-Protocol-Version "+v, http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMessageBytes))
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	resp := s.Handle(r.Context(), bytes.TrimSpace(body))
	if resp == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}
