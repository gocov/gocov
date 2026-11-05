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
	for _, k := range []string{"ignore_ms", "gate_base_ms", "blob_ms", "diff_ms", "insert_ms", "recompute_ms", "push_ms", "push_lock_ms", "accept_ms", "total_ms"} {
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

// The push is split in the log into the lock wait and each surface's
// round trip; the PR comment only for a PR, and only the lock wait when
// no surface reached the forge (no connection, superseded).
func TestAcceptLogSplitsThePush(t *testing.T) {
	ran := pushTimings{ran: true, lock: 30 * time.Millisecond, status: 400 * time.Millisecond, insights: 700 * time.Millisecond, comment: 900 * time.Millisecond}
	for _, tc := range []struct {
		name    string
		prID    string
		timings pushTimings
		want    map[string]float64 // absent keys must be left out
	}{
		{"pr", "7", ran, map[string]float64{"push_lock_ms": 30, "status_ms": 400, "insights_ms": 700, "comment_ms": 900}},
		{"branch build", "", ran, map[string]float64{"push_lock_ms": 30, "status_ms": 400, "insights_ms": 700}},
		{"nothing pushed", "7", pushTimings{lock: 30 * time.Millisecond}, map[string]float64{"push_lock_ms": 30}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			p := &Pipeline{Log: slog.New(slog.NewJSONHandler(&buf, nil))}
			sub := Submission{Repo: &store.Repo{Slug: "acme/widgets"}, Commit: "c1", PRID: tc.prID, Profile: prof("a.go", 1, 2)}
			res := &Result{Merged: &Merged{}, Push: PushResult{BuildStatus: "posted", timings: tc.timings}}
			p.logAccept(sub, newAcceptTimings(time.Time{}), res, nil)

			line := findLog(t, logLines(t, &buf), "upload accepted")
			for _, k := range []string{"push_lock_ms", "status_ms", "insights_ms", "comment_ms"} {
				want, ok := tc.want[k]
				got, present := line[k]
				switch {
				case ok && got != want:
					t.Errorf("%s = %v, want %v", k, got, want)
				case !ok && present:
					t.Errorf("%s = %v, want it left out", k, got)
				}
			}
		})
	}
}
