package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// GitHub App / Marketplace webhook (POST /github/webhook). Enabled only
// when GOCOV_GITHUB_WEBHOOK_SECRET is set; the route is public (GitHub is
// not signed in) but every request is authenticated by its HMAC-SHA256
// signature over the raw body.
//
// The listing on GitHub Marketplace requires this endpoint: GitHub
// delivers marketplace_purchase events here on plan changes. The free
// listing has nothing to provision, so those are logged for now — M4
// billing attaches real handling. installation events are used to flip
// the workspace's github_app_broken flag eagerly, instead of waiting for
// the next upload to discover a revoked install lazily (githubapp.go).

// webhookMaxBody caps the payload we read. GitHub hooks are small; this
// is a generous ceiling that still bounds memory.
const webhookMaxBody = 5 << 20 // 5 MiB

// webhookPayload is the subset of the GitHub webhook body gocov reads.
type webhookPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	MarketplacePurchase struct {
		Account struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
		Plan struct {
			Name string `json:"name"`
		} `json:"plan"`
	} `json:"marketplace_purchase"`
}

func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, webhookMaxBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if !s.validWebhookSignature(body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	if event == "ping" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, "malformed payload", http.StatusBadRequest)
		return
	}
	switch event {
	case "marketplace_purchase":
		s.log.Info("marketplace_purchase",
			"action", p.Action,
			"account", p.MarketplacePurchase.Account.Login,
			"account_type", p.MarketplacePurchase.Account.Type,
			"plan", p.MarketplacePurchase.Plan.Name)
		// Free listing: nothing to provision. M4 billing attaches here.
	case "installation":
		s.handleInstallationEvent(r.Context(), &p)
	default:
		s.log.Debug("github webhook ignored", "event", event, "action", p.Action)
	}
	// Always 2xx for authenticated, well-formed deliveries so GitHub does
	// not retry events we intentionally ignore.
	w.WriteHeader(http.StatusOK)
}

// validWebhookSignature checks the sha256=… HMAC header against the body.
func (s *Server) validWebhookSignature(body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(header[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.webhookSecret))
	mac.Write(body)
	return hmac.Equal(want, mac.Sum(nil))
}

// handleInstallationEvent flips github_app_broken for the workspace(s)
// linked to the installation. created/new_permissions are no-ops here —
// the /github/setup redirect already links and heals fresh installs.
func (s *Server) handleInstallationEvent(ctx context.Context, p *webhookPayload) {
	id := p.Installation.ID
	if id == 0 {
		return
	}
	switch p.Action {
	case "deleted", "suspend":
		s.forges.SetInstallationBroken(ctx, id, true)
	case "unsuspend":
		s.forges.SetInstallationBroken(ctx, id, false)
	default:
		s.log.Debug("github installation event", "action", p.Action, "installation", id)
	}
}
