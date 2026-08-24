package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gocov/gocov/internal/forge"
	"github.com/gocov/gocov/internal/store"
)

const backendPart = "mode: set\nexample.com/m/back.go:1.1,5.2 8 1\n"

const frontendPart = "mode: set\nexample.com/m/front.go:1.1,5.2 2 0\n"

func TestMergedReportAcrossParts(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	ctx := context.Background()

	// A single upload with no part is a one-part merged report identical to
	// the upload — backward compatibility.
	single := doUpload(t, f, "secret-token", map[string]string{"commit": "solo"}, testProfile)
	var sr uploadResponse
	if err := json.Unmarshal(single.Body.Bytes(), &sr); err != nil {
		t.Fatal(err)
	}
	cr, err := f.store.CommitReport(ctx, f.repo.ID, "solo")
	if err != nil {
		t.Fatal(err)
	}
	if cr.PartCount != 1 || cr.TotalPct != 80 || cr.TotalStmts != 10 || cr.CoveredStmts != 8 {
		t.Errorf("single-upload merged report = %+v, want 1 part 80%% 8/10", cr)
	}
	if sr.TotalPct != cr.TotalPct {
		t.Errorf("response %.1f != merged report %.1f", sr.TotalPct, cr.TotalPct)
	}

	// Backend part first: the merged report is backend alone, 8/8 = 100%.
	back := doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "part": "backend"}, backendPart)
	var br uploadResponse
	if err := json.Unmarshal(back.Body.Bytes(), &br); err != nil {
		t.Fatal(err)
	}
	if br.TotalPct != 100 || br.CoveredStmts != 8 || br.TotalStmts != 8 {
		t.Errorf("backend-only response = %.1f%% %d/%d, want 100%% 8/8", br.TotalPct, br.CoveredStmts, br.TotalStmts)
	}

	// Frontend part lands: the merged report now spans both, 8/10 = 80%.
	front := doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "part": "frontend"}, frontendPart)
	var fr uploadResponse
	if err := json.Unmarshal(front.Body.Bytes(), &fr); err != nil {
		t.Fatal(err)
	}
	if fr.TotalPct != 80 || fr.CoveredStmts != 8 || fr.TotalStmts != 10 {
		t.Errorf("merged response = %.1f%% %d/%d, want 80%% 8/10", fr.TotalPct, fr.CoveredStmts, fr.TotalStmts)
	}
	cr, err = f.store.CommitReport(ctx, f.repo.ID, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if cr.PartCount != 2 || cr.TotalPct != 80 || cr.TotalStmts != 10 {
		t.Errorf("merged report = %+v, want 2 parts 80%% total 10", cr)
	}

	// The build status the forge saw reflects the merged total, not the last
	// part uploaded (which alone was 0%).
	last := f.forge.StatusCalls[len(f.forge.StatusCalls)-1]
	if last.CommitSHA != "c1" || !strings.Contains(last.Status.Description, "80.0%") {
		t.Errorf("last status = %+v, want merged 80%% for c1", last.Status)
	}
}

func TestMergedReportReplaceNoDoubleCount(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	ctx := context.Background()

	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "part": "backend"}, backendPart)
	// Re-uploading the same (commit, part) — a CI retry — must replace, not
	// accumulate: the merged total stays 8/8, not 16/16.
	doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "part": "backend"}, backendPart)

	cr, err := f.store.CommitReport(ctx, f.repo.ID, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if cr.PartCount != 1 || cr.TotalStmts != 8 || cr.CoveredStmts != 8 {
		t.Errorf("after retry merged report = %+v, want 1 part 8/8 (no double count)", cr)
	}
}

func TestMergedReportConcurrentParts(t *testing.T) {
	// Parallel CI jobs upload distinct parts of one commit at the same time.
	// Without serialized recompute a slow recompute could clobber a newer
	// one and drop a part; the merged report must end with every part.
	f := newFixture(t, nil) // no forge creds: keep the test to the store/recompute path
	const n = 8

	type req struct {
		body *bytes.Buffer
		ct   string
	}
	reqs := make([]req, n)
	for i := range n {
		// Each part covers its own file, 1/1, so the merged commit is n/n.
		prof := fmt.Sprintf("mode: set\nexample.com/m/f%d.go:1.1,2.2 1 1\n", i)
		body, ct := multipartUpload(t, map[string]string{
			"commit": "c1", "branch": "main", "part": fmt.Sprintf("p%d", i),
		}, prof)
		reqs[i] = req{body, ct}
	}

	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/upload", reqs[i].body)
			r.Header.Set("Content-Type", reqs[i].ct)
			r.Header.Set("Authorization", "Bearer secret-token")
			rec := httptest.NewRecorder()
			f.srv.ServeHTTP(rec, r)
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	for i, c := range codes {
		if c != http.StatusCreated {
			t.Fatalf("concurrent upload %d: status %d", i, c)
		}
	}
	cr, err := f.store.CommitReport(context.Background(), f.repo.ID, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if cr.PartCount != n || cr.CoveredStmts != n || cr.TotalStmts != n || cr.TotalPct != 100 {
		t.Errorf("merged report = %+v, want %d parts and %d/%d = 100%% (no part dropped)", cr, n, n, n)
	}
}

func TestMergedGateSelfHeals(t *testing.T) {
	f := newFixture(t, map[string]string{"username": "u", "app_password": "p"})
	ctx := context.Background()
	f.repo.Gate = store.Gate{MinCoverage: new(float64(50))}
	if err := f.store.UpdateRepo(ctx, f.repo); err != nil {
		t.Fatal(err)
	}

	// The frontend part arrives first: 0/2 alone is below the 50% floor, so
	// the interim merged report fails the gate.
	first := doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "part": "frontend"}, frontendPart)
	var fr uploadResponse
	if err := json.Unmarshal(first.Body.Bytes(), &fr); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fr.Gate, "failed") {
		t.Errorf("interim gate = %q, want failed", fr.Gate)
	}
	if got := f.forge.StatusCalls[len(f.forge.StatusCalls)-1].Status.State; got != forge.StateFailed {
		t.Errorf("interim status state = %q, want failed", got)
	}
	if cr, _ := f.store.CommitReport(ctx, f.repo.ID, "c1"); cr == nil || !cr.GateFailed {
		t.Errorf("interim report should be gate-failed, got %+v", cr)
	}

	// The backend part lands: merged 8/10 = 80% clears the gate, and the
	// status is corrected in place — self-healing.
	second := doUpload(t, f, "secret-token", map[string]string{"commit": "c1", "part": "backend"}, backendPart)
	var sr uploadResponse
	if err := json.Unmarshal(second.Body.Bytes(), &sr); err != nil {
		t.Fatal(err)
	}
	if sr.Gate != "passed" {
		t.Errorf("healed gate = %q, want passed", sr.Gate)
	}
	if got := f.forge.StatusCalls[len(f.forge.StatusCalls)-1].Status.State; got != forge.StateSuccessful {
		t.Errorf("healed status state = %q, want successful", got)
	}
	if cr, _ := f.store.CommitReport(ctx, f.repo.ID, "c1"); cr == nil || cr.GateFailed {
		t.Errorf("healed report should pass the gate, got %+v", cr)
	}
}
