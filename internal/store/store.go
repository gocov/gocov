// Package store defines the storage interface and its domain types.
// Implementations: postgres (production), memory (tests).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/gocov/gocov/internal/diffcov"
	"github.com/gocov/gocov/internal/profile"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("store: not found")

// Gate holds the optional coverage requirements enforced on uploads.
// A nil field means that rule is not applied.
type Gate struct {
	// MinCoverage is the minimum acceptable total coverage percentage.
	MinCoverage *float64
	// MinDiffCoverage is the minimum acceptable diff coverage percentage
	// for PR uploads with executable changed lines.
	MinDiffCoverage *float64
	// MaxCoverageDrop is how many percentage points total coverage may
	// fall relative to the comparison baseline; 0 forbids any drop.
	MaxCoverageDrop *float64
}

// Configured reports whether any gate rule is set.
func (g Gate) Configured() bool {
	return g.MinCoverage != nil || g.MinDiffCoverage != nil || g.MaxCoverageDrop != nil
}

// Workspace groups repos under a slug prefix ("workspace" in
// "workspace/repo"). Its token authorizes uploads for every repo with
// that prefix; unknown repos are auto-registered on first upload.
type Workspace struct {
	ID     int64
	Forge  string // forge for auto-created repos, "bitbucket" for now
	Prefix string // e.g. "myworkspace"
	Token  string
	// DefaultBranch is assigned to auto-created repos when the forge
	// cannot be asked for the real one.
	DefaultBranch string
	// ForgeCredentials holds workspace-level forge secrets (same shape as
	// Repo.ForgeCredentials). They rank between repo credentials and the
	// global defaults. Nil or empty when not configured.
	ForgeCredentials map[string]string
	// Gate is copied to auto-created repos at registration time.
	Gate Gate
	// GitHubInstallationID links the workspace to a GitHub App
	// installation (One-Click Connect D3); 0 when not connected. When
	// set, installation tokens outrank every stored credential (D4).
	GitHubInstallationID int64
	// GitHubAppBroken marks a connection whose installation token was
	// refused — the app was uninstalled or suspended on GitHub. Set
	// lazily on the first failing mint, cleared on reconnect or when a
	// mint succeeds again; the settings page renders it as "reconnect".
	GitHubAppBroken bool
	// BitbucketGrantAccount is the username of the Bitbucket account
	// that granted the workspace connect (One-Click Connect D6); posts
	// visibly carry this identity (D8). Empty when not connected.
	BitbucketGrantAccount string
	// BitbucketRefreshToken is the grant's rotating refresh token,
	// encrypted at rest by the postgres store (AES-GCM under
	// GOCOV_SECRET_KEY). Empty when not connected — or when decryption
	// failed, in which case BitbucketGrantBroken is set on the loaded
	// struct so the UI asks for a reconnect instead of erroring.
	BitbucketRefreshToken string
	// BitbucketGrantBroken mirrors GitHubAppBroken for the Bitbucket
	// grant: set lazily when a refresh comes back invalid_grant.
	BitbucketGrantBroken bool
	// GitLabGrantAccount, GitLabRefreshToken and GitLabGrantBroken
	// mirror the Bitbucket grant fields for GitLab Connect: the granting
	// account's username (notes post as it), the rotating refresh token
	// (encrypted at rest; empty with the broken flag set when
	// undecryptable) and the lazily-set revocation flag.
	GitLabGrantAccount string
	GitLabRefreshToken string
	GitLabGrantBroken  bool
	CreatedAt          time.Time
}

// Repo is a tracked repository. Slug is namespaced ("workspace/repo").
type Repo struct {
	ID            int64
	Forge         string // "bitbucket" for now
	Slug          string
	Token         string // per-repo upload token
	DefaultBranch string
	// ForgeCredentials holds forge-specific secrets (e.g. bitbucket
	// username/app_password). Nil or empty when not configured.
	ForgeCredentials map[string]string
	// Gate holds the repo's coverage requirements; violations turn the
	// pushed build status into a failure.
	Gate      Gate
	CreatedAt time.Time
}

// Upload is one coverage report for a commit.
type Upload struct {
	ID           int64
	RepoID       int64
	CommitSHA    string
	Branch       string
	PRID         string // empty when not a PR build
	Format       string
	TotalPct     float64
	CoveredStmts int64
	TotalStmts   int64
	RawBlobKey   string // blobstore key of the raw profile
	// DiffCoverage is set for PR uploads when the PR diff could be
	// fetched from the forge; nil otherwise.
	DiffCoverage *diffcov.Result
	// GateFailed marks uploads that violated the coverage gate; they are
	// excluded from comparison baselines.
	GateFailed bool
	// PathPrefix maps profile paths to repo-relative paths (e.g. the Go
	// module path), as sent with the upload.
	PathPrefix string
	// Part names the slice of the commit this upload covers (e.g. "backend",
	// "frontend"). Uploads with no explicit part carry "default". The merged
	// report reads the latest upload per (commit, part), so re-uploading a
	// part replaces it rather than accumulating.
	Part      string
	CreatedAt time.Time
}

// User is a web UI account, identified by the forge account it signed in
// with. Users are provisioned just-in-time on first authorized login; no
// passwords are ever stored.
type User struct {
	ID    int64
	Forge string // "bitbucket" for now
	// ForgeUUID is the forge's stable account identifier (survives
	// renames). Unique per forge.
	ForgeUUID   string
	Email       string
	DisplayName string
	// ForgeWorkspaces are the workspace slugs the forge reported at the
	// last sign-in (M3/D3). OAuth tokens are discarded at login, so this
	// snapshot is what the registration page renders from; it goes stale
	// until the next login, never fresher.
	ForgeWorkspaces []string
	CreatedAt       time.Time
	LastLoginAt     time.Time
}

// Session is a server-side web UI session. Only a hash of the session
// token is stored, never the token itself.
type Session struct {
	TokenHash string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CommitReport is the merged coverage of a commit, derived from the latest
// upload of each part and recomputed on every upload. It is the source of
// truth for status, gate, PR comment, insights, badge and trend, so a
// commit whose parts arrive in separate CI jobs reports their combined
// total rather than whichever part uploaded last. A commit with a single
// upload has a single-part report equal to that upload.
type CommitReport struct {
	ID           int64
	RepoID       int64
	CommitSHA    string
	Branch       string
	PRID         string // empty when not a PR build
	TotalPct     float64
	CoveredStmts int64
	TotalStmts   int64
	// GateFailed marks reports that violated the coverage gate; they are
	// excluded from comparison baselines, the same rule uploads carried.
	GateFailed bool
	// DiffCoverage is the merged diff coverage for PR commits; nil otherwise.
	DiffCoverage *diffcov.Result
	// PartCount is how many parts (distinct upload parts) fed the report.
	PartCount int
	// UploadID is the latest upload that fed this report; the trend links a
	// point to its upload detail page through it.
	UploadID  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UploadFile is per-file coverage within an upload. Blocks keep the full
// normalized block data so diff coverage can be computed later.
type UploadFile struct {
	UploadID     int64
	Path         string
	Pct          float64
	CoveredStmts int64
	TotalStmts   int64
	Blocks       []profile.Block
}

// Store is the persistence interface used by the server.
type Store interface {
	CreateRepo(ctx context.Context, r *Repo) error
	// UpdateRepo replaces the stored row matching r.ID with r's fields.
	UpdateRepo(ctx context.Context, r *Repo) error
	// DeleteRepo removes a repo together with its uploads and per-file rows.
	// Raw profile blobs are not touched; callers clean those up first.
	DeleteRepo(ctx context.Context, id int64) error
	RepoByID(ctx context.Context, id int64) (*Repo, error)
	RepoBySlug(ctx context.Context, slug string) (*Repo, error)
	RepoByToken(ctx context.Context, token string) (*Repo, error)
	ListRepos(ctx context.Context) ([]*Repo, error)

	CreateWorkspace(ctx context.Context, w *Workspace) error
	// UpdateWorkspace replaces the stored row matching w.ID with w's fields.
	UpdateWorkspace(ctx context.Context, w *Workspace) error
	// DeleteWorkspace removes the workspace token; repos created through
	// it are left untouched.
	DeleteWorkspace(ctx context.Context, id int64) error
	WorkspaceByPrefix(ctx context.Context, prefix string) (*Workspace, error)
	WorkspaceByToken(ctx context.Context, token string) (*Workspace, error)
	ListWorkspaces(ctx context.Context) ([]*Workspace, error)
	// RegisterWorkspace creates the workspace and makes userID its first
	// member atomically — self-service registration (M3) must never leave
	// a workspace nobody can see.
	RegisterWorkspace(ctx context.Context, w *Workspace, userID int64) error
	// SetWorkspaceBitbucketGrant updates only the Bitbucket grant fields.
	// Bitbucket rotates refresh tokens on every use, so the swap must be
	// a single narrow UPDATE that cannot clobber (or be clobbered by) a
	// concurrent full-row settings save.
	SetWorkspaceBitbucketGrant(ctx context.Context, workspaceID int64, account, refreshToken string, broken bool) error
	// SetWorkspaceGitLabGrant is SetWorkspaceBitbucketGrant's GitLab
	// twin, with the same rotation-safety contract.
	SetWorkspaceGitLabGrant(ctx context.Context, workspaceID int64, account, refreshToken string, broken bool) error

	// SetUserWorkspaces replaces a user's workspace memberships with the
	// given set: memberships not listed are removed and listed ones are
	// added, so re-running it with the same IDs is a no-op. Called at login
	// to mirror the user's current forge membership (M2).
	SetUserWorkspaces(ctx context.Context, userID int64, workspaceIDs []int64) error
	// ListWorkspacesForUser returns the workspaces the user is a member of,
	// ordered by prefix.
	ListWorkspacesForUser(ctx context.Context, userID int64) ([]*Workspace, error)

	// UpsertUser creates the user on first login or, when a row with the
	// same forge+ForgeUUID exists, refreshes its email, display name,
	// forge workspaces and last-login time. ID, CreatedAt and LastLoginAt
	// are set on u.
	UpsertUser(ctx context.Context, u *User) error
	UserByID(ctx context.Context, id int64) (*User, error)
	ListUsers(ctx context.Context) ([]*User, error)
	// DeleteUser removes the user together with all their sessions.
	DeleteUser(ctx context.Context, id int64) error

	CreateSession(ctx context.Context, sess *Session) error
	// UserBySession resolves a session token hash to its user. Expired
	// sessions are treated as absent (ErrNotFound).
	UserBySession(ctx context.Context, tokenHash string) (*User, error)
	DeleteSession(ctx context.Context, tokenHash string) error

	// CreateUpload persists the upload and its per-file rows atomically,
	// setting u.ID and u.CreatedAt.
	CreateUpload(ctx context.Context, u *Upload, files []*UploadFile) error
	Upload(ctx context.Context, id int64) (*Upload, error)
	// ListUploads returns uploads newest first; limit <= 0 means all.
	ListUploads(ctx context.Context, repoID int64, limit int) ([]*Upload, error)
	// ListBranchUploads is ListUploads restricted to one branch.
	ListBranchUploads(ctx context.Context, repoID int64, branch string, limit int) ([]*Upload, error)
	UploadFiles(ctx context.Context, uploadID int64) ([]*UploadFile, error)
	// LatestUploadsPerPart returns the most recent upload for each distinct
	// part of a commit — the set the merged report is computed from. A
	// re-uploaded part supersedes its earlier uploads here.
	LatestUploadsPerPart(ctx context.Context, repoID int64, commitSHA string) ([]*Upload, error)
	// WithCommitReportTx serializes the recompute of one commit's merged
	// report against concurrent uploads of the same commit and runs fn's
	// reads and upsert as one atomic, locked unit — so a slow recompute can
	// neither interleave with nor clobber a newer one and drop a part. fn
	// must route all its store access through the passed CommitTx. Locks on
	// different commits never contend.
	WithCommitReportTx(ctx context.Context, repoID int64, commitSHA string, fn func(ctx context.Context, tx CommitTx) error) error
	// UpsertCommitReport creates or replaces the merged report for
	// (repo, commit), setting cr.ID, cr.CreatedAt and cr.UpdatedAt. The
	// first-seen creation time is preserved across recomputes.
	UpsertCommitReport(ctx context.Context, cr *CommitReport) error
	// CommitReport returns the merged report for a commit, or ErrNotFound.
	CommitReport(ctx context.Context, repoID int64, commitSHA string) (*CommitReport, error)
	// LatestCommitReport returns the most recent merged report on a branch.
	LatestCommitReport(ctx context.Context, repoID int64, branch string) (*CommitReport, error)
	// LatestPassedCommitReport returns the most recent gate-passing merged
	// report on a branch, skipping excludeCommit (the commit being uploaded,
	// whose own in-progress report must not serve as its baseline). Used as
	// the delta and gate-drop baseline.
	LatestPassedCommitReport(ctx context.Context, repoID int64, branch, excludeCommit string) (*CommitReport, error)
	// ListBranchCommitReports returns merged reports on a branch newest
	// first; limit <= 0 means all. Feeds the coverage trend.
	ListBranchCommitReports(ctx context.Context, repoID int64, branch string, limit int) ([]*CommitReport, error)
	// TryPushStatus serializes forge status/PR-comment pushes for one commit
	// and runs push only if version is at least the last successfully pushed
	// version, recording version only after push returns nil. It closes the
	// window where a slow older push lands after a newer one, and a failed
	// push leaves the version untouched so a later part retries. push runs
	// with the per-commit lock held and does the forge HTTP itself, bounded
	// by ctx. Returns whether push ran.
	TryPushStatus(ctx context.Context, repoID int64, commitSHA string, version int64, push func(context.Context) error) (pushed bool, err error)
	// CommitParts returns the distinct part names uploaded for a commit —
	// a cheap read (no blocks/diff) for the per-commit parts cap.
	CommitParts(ctx context.Context, repoID int64, commitSHA string) ([]string, error)
}

// CommitTx is the store access available inside WithCommitReportTx. On
// Postgres every call runs on the one locked transaction, so the recompute's
// reads and its upsert are consistent and cannot deadlock on a second pooled
// connection.
type CommitTx interface {
	LatestUploadsPerPart(ctx context.Context, repoID int64, commitSHA string) ([]*Upload, error)
	UploadFiles(ctx context.Context, uploadID int64) ([]*UploadFile, error)
	LatestPassedCommitReport(ctx context.Context, repoID int64, branch, excludeCommit string) (*CommitReport, error)
	UpsertCommitReport(ctx context.Context, cr *CommitReport) error
}
