// The web UI's JSON API (/api/ui/): what the single-page app (spa.go) reads
// and writes, authenticated by the same session cookie as the pages. Unlike
// /api/v1 it is private to the bundled UI and versioned with it — no
// compatibility promise. Its shapes are declared once, for both sides, in
// web/src/lib/api/types.ts.
//
// Every response is an explicit DTO. The store's rows carry upload tokens
// and connection state, so a handler never marshals one directly: it copies
// out the fields the UI shows.

package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gocov/gocov/internal/store"
)

const (
	apiUIPrefix = "/api/ui/"
	// The endpoints a signed-out browser may read: how the app learns that
	// it is signed out, and what the sign-in page offers.
	apiSessionPath = "/api/ui/session"
	apiLoginPath   = "/api/ui/login"
)

func (s *Server) apiRoutes() {
	// Mutations ride the session cookie, so they must come from our own
	// pages: CrossOriginProtection rejects cross-site unsafe requests by
	// Sec-Fetch-Site/Origin and lets safe methods through untouched.
	guard := http.NewCrossOriginProtection()
	handle := func(pattern string, h http.HandlerFunc) {
		s.mux.Handle(pattern, guard.Handler(h))
	}
	handle("GET /api/ui/session", s.handleAPISession)
	handle("GET /api/ui/login", s.handleAPILogin)
	handle("GET /api/ui/dashboard", s.handleAPIDashboard)
	// The report surface mirrors the pages path for path, so the
	// signed-out pass-through (session.go) recognizes both spellings and a
	// public repo reads the same way through either.
	handle("GET /api/ui/repos/{forge}/{slug...}", s.handleAPIRepo)
	handle("GET /api/ui/uploads/{id}", s.handleAPIUpload)
	handle("GET /api/ui/uploads/{id}/files/{path...}", s.handleAPISource)
	// Onboarding: the workspace picker, the claim it posts, and the setup
	// screen that waits for the first report.
	handle("GET /api/ui/onboarding", s.handleAPIOnboarding)
	handle("POST /api/ui/onboarding/register", s.handleAPIRegister)
	// A GitLab workspace prefix carries slashes like a repo slug does, so
	// the workspace endpoints take the same shape as the repo ones below:
	// the trailing {prefix...} wildcard, the verb before it.
	handle("GET /api/ui/workspace-setup/{forge}/{prefix...}", s.handleAPIWorkspaceSetup)
	handle("GET /api/ui/workspace-setup-status/{forge}/{prefix...}", s.handleAPIWorkspaceSetupStatus)
	handle("GET /api/ui/workspace-settings/{forge}/{prefix...}", s.handleAPIWorkspace)
	handle("POST /api/ui/workspace-settings/save/{forge}/{prefix...}", s.handleAPIWorkspaceSettings)
	handle("POST /api/ui/workspace-settings/rotate-token/{forge}/{prefix...}", s.handleAPIWorkspaceRotate)
	handle("POST /api/ui/workspace-settings/reveal-token/{forge}/{prefix...}", s.handleAPIWorkspaceReveal)
	handle("POST /api/ui/workspace-settings/disconnect/{forge}/{prefix...}", s.handleAPIWorkspaceDisconnect)
	handle("POST /api/ui/workspace-settings/delete/{forge}/{prefix...}", s.handleAPIWorkspaceDelete)
	// Repo slugs carry a slash, so they ride as the trailing {slug...}
	// wildcard and the mutating verb goes before them — the same shape the
	// pages use (see the route table in server.go).
	handle("GET /api/ui/repo-settings/{forge}/{slug...}", s.handleAPIRepoSettings)
	handle("POST /api/ui/repo-settings/save/{forge}/{slug...}", s.handleAPIRepoSettingsSave)
	handle("POST /api/ui/repo-settings/rotate-token/{forge}/{slug...}", s.handleAPIRepoRotateToken)
	handle("POST /api/ui/repo-settings/reveal-token/{forge}/{slug...}", s.handleAPIRepoRevealToken)
	handle("POST /api/ui/repo-settings/delete/{forge}/{slug...}", s.handleAPIRepoDelete)
	// Unknown API paths answer in JSON rather than with the HTML 404 page.
	handle(apiUIPrefix, func(w http.ResponseWriter, _ *http.Request) {
		httpError(w, http.StatusNotFound, "not found")
	})
}

// apiUIPath reports whether a path belongs to the UI API, where a missing
// session is a 401 for the app to act on rather than a login redirect.
func apiUIPath(p string) bool { return strings.HasPrefix(p, apiUIPrefix) }

// apiSignedOutPath reports whether a UI API path answers without a session.
func apiSignedOutPath(p string) bool { return p == apiSessionPath || p == apiLoginPath }

// maxAPIBody bounds a UI API request body. Everything the app posts is a
// settings form: a handful of numbers, a branch name and a few lines of
// ignore patterns.
const maxAPIBody = 64 << 10

// readJSON decodes a request body into v, answering the client and
// returning false when it cannot. Unknown fields are refused rather than
// dropped, so a field the app renamed fails loudly instead of silently
// saving the old value.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAPIBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpError(w, http.StatusBadRequest, "malformed request body")
		return false
	}
	return true
}

// invalid answers a request the handler understood but cannot carry out:
// a percentage out of range, an empty branch name, an unparseable ignore
// pattern. The message is the one the settings pages show.
func invalid(w http.ResponseWriter, msg string) {
	httpError(w, http.StatusUnprocessableEntity, "%s", msg)
}

// The report DTOs shared by more than one endpoint — the repo page and
// the upload page describe the same coverage in the same words.

// repoRefDTO names a repo the way its URLs do.
type repoRefDTO struct {
	Forge string `json:"forge"`
	Slug  string `json:"slug"`
}

func newRepoRefDTO(repo *store.Repo) repoRefDTO {
	return repoRefDTO{Forge: repo.Forge, Slug: repo.Slug}
}

// gateDTO is a coverage gate as the app reads and writes it: three
// optional percentages, null where the rule is not set.
type gateDTO struct {
	MinCoverage     *float64 `json:"min_coverage"`
	MinDiffCoverage *float64 `json:"min_diff_coverage"`
	MaxCoverageDrop *float64 `json:"max_coverage_drop"`
}

func newGateDTO(g store.Gate) gateDTO {
	return gateDTO{MinCoverage: g.MinCoverage, MinDiffCoverage: g.MinDiffCoverage, MaxCoverageDrop: g.MaxCoverageDrop}
}

// gate is the posted gate as the store holds it. Its values still need
// validGate before they are saved.
func (g gateDTO) gate() store.Gate {
	return store.Gate{MinCoverage: g.MinCoverage, MinDiffCoverage: g.MinDiffCoverage, MaxCoverageDrop: g.MaxCoverageDrop}
}

// baseRefDTO is the report a verdict is measured against.
type baseRefDTO struct {
	UploadID int64   `json:"upload_id"`
	SHA      string  `json:"sha"`
	Coverage float64 `json:"coverage"`
}

// verdictDTO is a coverage standing against the repo's gate: the state,
// the numbers behind it and the prose the gate itself wrote.
type verdictDTO struct {
	State    string      `json:"state"`
	Coverage float64     `json:"coverage"`
	Delta    *float64    `json:"delta"`
	Reason   string      `json:"reason"`
	Base     *baseRefDTO `json:"base"`
}

// against records the report the verdict is measured against, and the
// delta to it.
func (v *verdictDTO) against(uploadID int64, sha string, basePct float64) {
	v.Delta = new(v.Coverage - basePct)
	v.Base = &baseRefDTO{UploadID: uploadID, SHA: sha, Coverage: basePct}
}

// fileRowDTO is one file of an upload with its baseline comparison. The
// tree the files card draws is built client-side from these rows.
type fileRowDTO struct {
	Path         string   `json:"path"`
	Coverage     float64  `json:"coverage"`
	CoveredStmts int64    `json:"covered_stmts"`
	TotalStmts   int64    `json:"total_stmts"`
	Uncovered    string   `json:"uncovered"`
	Before       *float64 `json:"before"`
	// The baseline's own statement counts, null with Before. A directory's
	// "before" is a rollup of these, not an average of its files' percentages.
	BeforeCoveredStmts *int64 `json:"before_covered_stmts"`
	BeforeTotalStmts   *int64 `json:"before_total_stmts"`
	NewFile            bool   `json:"new_file"`
	NewlyUncovered     string `json:"newly_uncovered"`
	SourceChanged      bool   `json:"source_changed"`
	CoverageChanged    bool   `json:"coverage_changed"`
}

// filesViewDTO is the files card: which upload the rows came from, and
// whether there was a baseline to compare them against.
type filesViewDTO struct {
	UploadID int64        `json:"upload_id"`
	HasBase  bool         `json:"has_base"`
	Files    []fileRowDTO `json:"files"`
}

// optPct is a percentage the app must be able to tell apart from zero:
// "no coverage yet" is null, not 0.
func optPct(has bool, pct float64) *float64 {
	if !has {
		return nil
	}
	return &pct
}

func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Error("encoding api response", "err", err)
	}
}

// sessionDTO is what the app shell needs before it renders anything: who is
// looking, and how this instance is set up.
type sessionDTO struct {
	User *userDTO `json:"user"`
	// AuthEnabled false is the open instance: no sign-in, and the shell
	// says so.
	AuthEnabled bool          `json:"auth_enabled"`
	Hosted      bool          `json:"hosted"`
	Analytics   *analyticsDTO `json:"analytics,omitempty"`
}

type userDTO struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

type analyticsDTO struct {
	Key    string `json:"key"`
	Host   string `json:"host"`
	UserID string `json:"user_id,omitempty"`
}

// handleAPISession implements GET /api/ui/session.
func (s *Server) handleAPISession(w http.ResponseWriter, r *http.Request) {
	dto := sessionDTO{AuthEnabled: s.authEnabled(), Hosted: s.hosted}
	u := currentUser(r)
	if u != nil {
		dto.User = &userDTO{DisplayName: u.DisplayName, Email: u.Email}
	}
	if s.posthog.Configured() {
		dto.Analytics = &analyticsDTO{Key: s.posthog.Key, Host: s.posthog.Host}
		if u != nil {
			dto.Analytics.UserID = strconv.FormatInt(u.ID, 10)
		}
	}
	s.writeJSON(w, dto)
}
