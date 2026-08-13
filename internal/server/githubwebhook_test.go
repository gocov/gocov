package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	blobmem "github.com/gocov/gocov/internal/blobstore/memory"
	"github.com/gocov/gocov/internal/profile"
	"github.com/gocov/gocov/internal/store"
	storemem "github.com/gocov/gocov/internal/store/memory"
)

const webhookSecret = "shhh"

// webhookServer builds a server with the webhook enabled and one github
// workspace linked to installation 4242.
func webhookServer(t *testing.T) (*Server, *storemem.Store) {
	t.Helper()
	st := storemem.New()
	if err := st.CreateWorkspace(context.Background(), &store.Workspace{
		Forge: "github", Prefix: "acme", Token: "tok", DefaultBranch: "main",
		GitHubInstallationID: 4242,
	}); err != nil {
		t.Fatal(err)
	}
	srv := New(Config{
		Store:               st,
		Blobs:               blobmem.New(),
		Parsers:             map[string]profile.Parser{"go": profile.GoParser{}},
		GitHubWebhookSecret: webhookSecret,
	})
	return srv, st
}

func postWebhook(srv *Server, event, body, sig string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/github/webhook", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	if sig != "" {
		req.Header.Set("X-Hub-Signature-256", sig)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	srv, _ := webhookServer(t)
	body := `{"action":"purchased"}`

	if rec := postWebhook(srv, "marketplace_purchase", body, "sha256=deadbeef"); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong signature: status = %d, want 401", rec.Code)
	}
	if rec := postWebhook(srv, "marketplace_purchase", body, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("missing signature: status = %d, want 401", rec.Code)
	}
	// A signature over different bytes must not verify.
	if rec := postWebhook(srv, "marketplace_purchase", body, sign(webhookSecret, "tampered")); rec.Code != http.StatusUnauthorized {
		t.Errorf("tampered body: status = %d, want 401", rec.Code)
	}
}

func TestWebhookMarketplacePurchase(t *testing.T) {
	srv, _ := webhookServer(t)
	body := `{"action":"purchased","marketplace_purchase":{"account":{"login":"acme","type":"Organization"},"plan":{"name":"Free"}}}`
	rec := postWebhook(srv, "marketplace_purchase", body, sign(webhookSecret, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("marketplace_purchase: status = %d, want 200", rec.Code)
	}
}

func TestWebhookPingAndUnknownEvent(t *testing.T) {
	srv, _ := webhookServer(t)
	for _, event := range []string{"ping", "push"} {
		body := `{"zen":"go"}`
		if rec := postWebhook(srv, event, body, sign(webhookSecret, body)); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", event, rec.Code)
		}
	}
}

func TestWebhookInstallationFlipsBrokenFlag(t *testing.T) {
	srv, st := webhookServer(t)

	broken := func() bool {
		ws, err := st.WorkspaceByPrefix(context.Background(), "acme")
		if err != nil {
			t.Fatal(err)
		}
		return ws.GitHubAppBroken
	}

	del := `{"action":"deleted","installation":{"id":4242}}`
	if rec := postWebhook(srv, "installation", del, sign(webhookSecret, del)); rec.Code != http.StatusOK {
		t.Fatalf("installation deleted: status = %d", rec.Code)
	}
	if !broken() {
		t.Error("installation deleted did not mark the workspace broken")
	}

	heal := `{"action":"unsuspend","installation":{"id":4242}}`
	if rec := postWebhook(srv, "installation", heal, sign(webhookSecret, heal)); rec.Code != http.StatusOK {
		t.Fatalf("installation unsuspend: status = %d", rec.Code)
	}
	if broken() {
		t.Error("installation unsuspend did not heal the workspace")
	}

	// An event for an unrelated installation must not touch our workspace.
	other := `{"action":"deleted","installation":{"id":9999}}`
	if rec := postWebhook(srv, "installation", other, sign(webhookSecret, other)); rec.Code != http.StatusOK {
		t.Fatalf("unrelated installation: status = %d", rec.Code)
	}
	if broken() {
		t.Error("unrelated installation event flipped the flag")
	}
}

func TestWebhookRouteAbsentWithoutSecret(t *testing.T) {
	st := storemem.New()
	srv := New(Config{
		Store:   st,
		Blobs:   blobmem.New(),
		Parsers: map[string]profile.Parser{"go": profile.GoParser{}},
		// No GitHubWebhookSecret.
	})
	body := `{"action":"purchased"}`
	if rec := postWebhook(srv, "marketplace_purchase", body, sign(webhookSecret, body)); rec.Code != http.StatusNotFound {
		t.Errorf("webhook without secret: status = %d, want 404", rec.Code)
	}
}
