package core

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/store"
)

// logLines decodes every JSON log line written to buf.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(buf)
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

// findLog returns the one line with message msg, failing when there is not
// exactly one.
func findLog(t *testing.T, lines []map[string]any, msg string) map[string]any {
	t.Helper()
	var found []map[string]any
	for _, l := range lines {
		if l["msg"] == msg {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d %q lines in %v, want 1", len(found), msg, lines)
	}
	return found[0]
}

// A landed upload logs one line carrying every stage's time, the request's
// own share when the transport said when it started, and what the merge
// and the forge push came to.
func TestAcceptLogsStageTimings(t *testing.T) {
	p, _, repo := newPipeline(t, store.Gate{})
	p.Blobs = blobmem.New()
	var buf bytes.Buffer
	p.Log = slog.New(slog.NewJSONHandler(&buf, nil))

	_, err := p.Accept(t.Context(), Submission{
		Repo: repo, Commit: "c1", Branch: "main", PRID: "7", Part: "backend", Format: "go",
		Raw: []byte("mode: set\n"), Profile: prof("a.go", 8, 10),
		Started: time.Now().Add(-50 * time.Millisecond),
	})
	if err != nil {
		t.Fatal(err)
	}

	line := findLog(t, logLines(t, &buf), "upload accepted")
	if line["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", line["level"])
	}
	want := map[string]any{
		"repo": "acme/widgets", "commit": "c1", "part": "backend",
		"files": 1.0, "bytes": 10.0, "parts": 1.0, "build_status": "skipped",
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("%s = %v, want %v", k, line[k], v)
		}
	}
	for _, k := range []string{"ignore_ms", "gate_base_ms", "blob_ms", "diff_ms", "insert_ms", "recompute_ms", "push_ms", "accept_ms", "total_ms"} {
		if _, ok := line[k].(float64); !ok {
			t.Errorf("%s missing or not a number: %v", k, line[k])
		}
	}
	if ms, _ := line["read_parse_ms"].(float64); ms < 50 {
		t.Errorf("read_parse_ms = %v, want at least the 50ms before Accept", line["read_parse_ms"])
	}
}

// Without a start time from the transport, or a PR, the fields that
// depend on them are left out rather than logged as zero.
func TestAcceptLogOmitsUnknownFields(t *testing.T) {
	p, _, repo := newPipeline(t, store.Gate{})
	p.Blobs = blobmem.New()
	var buf bytes.Buffer
	p.Log = slog.New(slog.NewJSONHandler(&buf, nil))

	if _, err := p.Accept(t.Context(), Submission{
		Repo: repo, Commit: "c1", Branch: "main", Part: "default", Format: "go",
		Raw: []byte("mode: set\n"), Profile: prof("a.go", 8, 10),
	}); err != nil {
		t.Fatal(err)
	}
	line := findLog(t, logLines(t, &buf), "upload accepted")
	for _, k := range []string{"read_parse_ms", "total_ms", "diff_ms"} {
		if v, ok := line[k]; ok {
			t.Errorf("%s = %v, want it left out", k, v)
		}
	}
}

// A refused upload logs at warn, naming the stage it failed in.
func TestAcceptLogsFailingStage(t *testing.T) {
	p, _, repo := newPipeline(t, store.Gate{})
	p.Blobs = blobmem.New()
	var buf bytes.Buffer
	p.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	repo.IgnorePaths = []string{"**"}

	if _, err := p.Accept(t.Context(), Submission{
		Repo: repo, Commit: "c1", Branch: "main", Part: "default", Format: "go",
		Raw: []byte("mode: set\n"), Profile: prof("example.com/m/a.go", 1, 2),
	}); err == nil {
		t.Fatal("an all-ignored upload was accepted")
	}
	line := findLog(t, logLines(t, &buf), "upload failed")
	if line["level"] != "WARN" || line["stage"] != "ignore" || line["err"] == nil {
		t.Errorf("failure line = %v, want WARN at stage ignore with the error", line)
	}
}
