// Package core is gocov's coverage logic, with no HTTP in it.
//
// It owns what happens to a coverage profile once it has arrived: the gate
// that decides whether a commit passes, the merge that rebuilds a commit's
// report from the parts uploaded for it, and the report gocov publishes
// back to the forge. internal/server keeps the transport — reading the
// multipart upload, rendering the pages, the cookies — and calls in here
// for every decision.
//
// The package deliberately imports neither net/http nor html/template, and
// a test enforces that: the moment a decision needs a request or a
// template, it belongs on the other side of the line.
package core

import (
	"log/slog"
	"sync"
	"time"

	"github.com/gocov/gocov/internal/blobstore"
	"github.com/gocov/gocov/internal/store"
)

// Pipeline is the store-backed half of the coverage logic: everything that
// has to read or write the database to answer a question about a commit.
// The stateless rules — evaluating a gate, wording its verdict — are plain
// functions beside it.
type Pipeline struct {
	Store store.Store
	Blobs blobstore.Store
	Log   *slog.Logger
	// BaseURL is the public URL of this instance; the forge surfaces link
	// back to it, so it is part of what gets published, not transport.
	BaseURL string
	// Forges resolves the client a repo's workspace is connected through.
	// Nil leaves every forge surface reporting "skipped".
	Forges *Forges
	// Hosted marks the self-service deployment; the PR comment then
	// carries the one-line gocov signature. Self-hosted instances never
	// get marketing appended to their PRs.
	Hosted bool

	// VisibilityUploadTTL and VisibilityServeTTL override how long the
	// cached forge visibility answer counts as fresh on the upload and
	// anonymous serving paths (repo.go). Zero means the defaults; only
	// tests shrink them.
	VisibilityUploadTTL time.Duration
	VisibilityServeTTL  time.Duration

	// visMu guards visChecks: when each repo's visibility was last
	// re-checked in the background, the serving path's rate limit.
	visMu     sync.Mutex
	visChecks map[int64]time.Time
}
