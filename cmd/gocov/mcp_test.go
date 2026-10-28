package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// mcpSession feeds the server one message per line and returns its
// answers, one decoded message per line written.
func mcpSession(t *testing.T, server string, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	if err := runMCP([]string{"-server", server}, in, &out, mapEnv(nil), noGit, http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	var msgs []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("answer is not one JSON message per line: %q", line)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

// toolText is a tools/call result's text and whether it is a tool error.
func toolText(t *testing.T, msg map[string]any) (string, bool) {
	t.Helper()
	res, ok := msg["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", msg)
	}
	content := res["content"].([]any)
	isErr, _ := res["isError"].(bool)
	return content[0].(map[string]any)["text"].(string), isErr
}

func TestMCPHandshake(t *testing.T) {
	msgs := mcpSession(t, "https://unused.example",
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		``,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
		`{"jsonrpc":"2.0","id":"p","method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
	)
	// The notification and the blank line get no answer.
	if len(msgs) != 4 {
		t.Fatalf("got %d answers, want 4: %v", len(msgs), msgs)
	}
	init := msgs[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-03-26" {
		t.Errorf("a version it speaks: got %v back", init["protocolVersion"])
	}
	if info := init["serverInfo"].(map[string]any); info["name"] != "gocov" {
		t.Errorf("serverInfo = %v", info)
	}
	if _, ok := init["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("capabilities = %v, want tools", init["capabilities"])
	}
	if v := msgs[1]["result"].(map[string]any)["protocolVersion"]; v != mcpProtocolVersions[0] {
		t.Errorf("a version it does not speak: got %v, want the newest", v)
	}
	if msgs[2]["id"] != "p" || msgs[2]["result"] == nil {
		t.Errorf("ping = %v", msgs[2])
	}
	tools := msgs[3]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "get_report" {
		t.Errorf("tools = %v", tools)
	}
}

func TestMCPGetReport(t *testing.T) {
	srv, req := newReportServer(t, http.StatusOK, reportBody)
	msgs := mcpSession(t, srv.URL,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_report","arguments":{"forge":"github","repo":"acme/widgets","commit":"7aa8f0162ab5c0ffee"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_report","arguments":{"forge":"github","repo":"acme/widgets","branch":"main","format":"json"}}}`,
	)
	text, isErr := toolText(t, msgs[0])
	if isErr || !strings.Contains(text, "uncovered changed lines:\n  cmd/gocov/oidc.go: 49-50, 57-58\n") {
		t.Errorf("text answer (error %v):\n%s", isErr, text)
	}
	if text, _ := toolText(t, msgs[1]); text != reportBody {
		t.Errorf("json answer rewrote the body:\n%s", text)
	}
	if req.URL.Query().Get("branch") != "main" {
		t.Errorf("last request %s, want ?branch=main", req.URL)
	}
}

// A report that cannot be read is the tool's error, which the agent reads,
// not the protocol's.
func TestMCPGetReportFailures(t *testing.T) {
	missing, _ := newReportServer(t, http.StatusNotFound, `{"error":"not found"}`)
	garbled, _ := newReportServer(t, http.StatusOK, `not json`)
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_report","arguments":{"forge":"github","repo":"acme/widgets","commit":"abc"}}}`

	for _, tc := range []struct {
		name, server, want string
	}{
		{"no report", missing.URL, "not uploaded yet"},
		{"not a report", garbled.URL, "decoding report"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, isErr := toolText(t, mcpSession(t, tc.server, call)[0])
			if !isErr || !strings.Contains(text, tc.want) {
				t.Errorf("isError = %v, text = %q, want it to mention %q", isErr, text, tc.want)
			}
		})
	}
	// Nothing to detect from and nothing given: said, not guessed.
	text, isErr := toolText(t, mcpSession(t, missing.URL, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_report"}}`)[0])
	if !isErr || !strings.Contains(text, "set -repo") {
		t.Errorf("isError = %v, text = %q", isErr, text)
	}
}

func TestMCPProtocolErrors(t *testing.T) {
	msgs := mcpSession(t, "https://unused.example",
		`{not json`,
		`{"id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete_everything"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_report","arguments":"repo"}}`,
	)
	want := []float64{rpcParseError, rpcInvalidRequest, rpcMethodNotFound, rpcInvalidParams, rpcInvalidParams}
	if len(msgs) != len(want) {
		t.Fatalf("got %d answers, want %d: %v", len(msgs), len(want), msgs)
	}
	for i, m := range msgs {
		e, ok := m["error"].(map[string]any)
		if !ok || e["code"] != want[i] {
			t.Errorf("answer %d = %v, want error %v", i, m, want[i])
		}
	}
	// An unreadable message has no id to answer: null, per JSON-RPC.
	if id, ok := msgs[0]["id"]; !ok || id != nil {
		t.Errorf("parse error id = %v, want null", msgs[0]["id"])
	}
}

func TestMCPFlags(t *testing.T) {
	if err := runMCP([]string{"stray"}, strings.NewReader(""), &bytes.Buffer{}, mapEnv(nil), noGit, http.DefaultClient); err == nil {
		t.Error("a stray argument was accepted")
	}
	if err := runMCP([]string{"-nope"}, strings.NewReader(""), &bytes.Buffer{}, mapEnv(nil), noGit, http.DefaultClient); err == nil {
		t.Error("an unknown flag was accepted")
	}
	if err := run([]string{"mcp", "stray"}); err == nil || !strings.Contains(err.Error(), "gocov mcp") {
		t.Errorf("run mcp: err = %v, want the mcp usage", err)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

// A client that has gone away ends the session with the write's error.
func TestMCPClientGone(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	if err := runMCP(nil, in, brokenWriter{}, mapEnv(nil), noGit, http.DefaultClient); err == nil || !strings.Contains(err.Error(), "pipe closed") {
		t.Errorf("err = %v, want the write error", err)
	}
}
