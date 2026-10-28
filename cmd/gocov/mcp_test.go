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
	// Arguments of the wrong type are the tool's error too.
	text, isErr := toolText(t, mcpSession(t, missing.URL, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_report","arguments":{"repo":5}}}`)[0])
	if !isErr || !strings.Contains(text, "invalid arguments") {
		t.Errorf("isError = %v, text = %q", isErr, text)
	}
	// Nothing to detect from and nothing given: said, not guessed.
	text, isErr = toolText(t, mcpSession(t, missing.URL, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_report"}}`)[0])
	if !isErr || !strings.Contains(text, "set -repo") {
		t.Errorf("isError = %v, text = %q", isErr, text)
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
