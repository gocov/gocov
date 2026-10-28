package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// echoServer offers one tool that answers with its arguments, or fails
// when asked to.
func echoServer() *Server {
	return &Server{Name: "test", Version: "v1", Tools: []Tool{{
		Name: "echo", Title: "Echo", Description: "says it back",
		InputSchema: map[string]any{"type": "object"},
		Call: func(_ context.Context, args json.RawMessage) (string, error) {
			if strings.Contains(string(args), "fail") {
				return "", errors.New("it failed")
			}
			return "got " + string(args), nil
		},
	}}}
}

// stdio feeds the server one message per line and decodes its answers.
func stdio(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := echoServer().ServeStdio(t.Context(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
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

func result(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	r, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", m)
	}
	return r
}

func TestHandshake(t *testing.T) {
	msgs := stdio(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`,
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
	init := result(t, msgs[0])
	if init["protocolVersion"] != "2025-03-26" {
		t.Errorf("a version it speaks: got %v back", init["protocolVersion"])
	}
	if info := init["serverInfo"].(map[string]any); info["name"] != "test" || info["version"] != "v1" {
		t.Errorf("serverInfo = %v", info)
	}
	if _, ok := init["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("capabilities = %v, want tools", init["capabilities"])
	}
	if v := result(t, msgs[1])["protocolVersion"]; v != ProtocolVersions[0] {
		t.Errorf("a version it does not speak: got %v, want the newest", v)
	}
	if msgs[2]["id"] != "p" {
		t.Errorf("ping = %v", msgs[2])
	}
	tools := result(t, msgs[3])["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "echo" || tools[0].(map[string]any)["inputSchema"] == nil {
		t.Errorf("tools = %v", tools)
	}
}

func TestToolCall(t *testing.T) {
	msgs := stdio(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"a":1}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":null}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo","arguments":{"x":"fail"}}}`,
	)
	for i, want := range []string{`got {"a":1}`, "got ", "got ", "it failed"} {
		r := result(t, msgs[i])
		text := r["content"].([]any)[0].(map[string]any)["text"]
		if text != want {
			t.Errorf("call %d: text = %q, want %q", i+1, text, want)
		}
		if isErr, _ := r["isError"].(bool); isErr != (i == 3) {
			t.Errorf("call %d: isError = %v", i+1, isErr)
		}
	}
}

func TestProtocolErrors(t *testing.T) {
	msgs := stdio(t,
		`{not json`,
		`{"id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete_everything"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo","arguments":"repo"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":"echo"}`,
	)
	want := []float64{CodeParseError, CodeInvalidRequest, CodeMethodNotFound, CodeInvalidParams, CodeInvalidParams, CodeInvalidParams}
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

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

// A client that has gone away ends the session with the write's error.
func TestStdioClientGone(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	if err := echoServer().ServeStdio(t.Context(), in, brokenWriter{}); err == nil || !strings.Contains(err.Error(), "pipe closed") {
		t.Errorf("err = %v, want the write error", err)
	}
}

func post(t *testing.T, body string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	echoServer().ServeHTTP(rec, req)
	return rec
}

func TestStreamableHTTP(t *testing.T) {
	rec := post(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"a":1}}}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status = %d, Content-Type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["id"] != 1.0 || result(t, m)["content"] == nil {
		t.Errorf("answer = %v", m)
	}

	// A notification is accepted with no body.
	if rec := post(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, "MCP-Protocol-Version", "2025-06-18"); rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
		t.Errorf("notification: %d %q", rec.Code, rec.Body)
	}
	// A revision it does not speak is refused before anything runs.
	if rec := post(t, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, "MCP-Protocol-Version", "1999-01-01"); rec.Code != http.StatusBadRequest {
		t.Errorf("unsupported version: status = %d", rec.Code)
	}
	if rec := post(t, `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"`+strings.Repeat("x", maxMessageBytes)+`"}}`); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized message: status = %d", rec.Code)
	}
	// No server-initiated stream, no sessions to end.
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		rec := httptest.NewRecorder()
		echoServer().ServeHTTP(rec, httptest.NewRequest(method, "/mcp", nil))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
			t.Errorf("%s: status = %d, Allow = %q", method, rec.Code, rec.Header().Get("Allow"))
		}
	}
}
