// Package memory provides an in-memory store.Store for tests.
package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gocov/gocov/internal/store"
)

// Store is an in-memory implementation of store.Store. Safe for concurrent use.
type Store struct {
	mu         sync.Mutex
	repoSeq    int64
	upSeq      int64
	wsSeq      int64
	userSeq    int64
	crSeq      int64
	repos      map[int64]*store.Repo
	uploads    map[int64]*store.Upload
	files      map[int64][]*store.UploadFile // keyed by upload ID
	reports    map[int64]*store.CommitReport // merged reports, keyed by report ID
	crLocks    map[string]*sync.Mutex        // per-commit recompute locks
	crStatus   map[string]int64              // last claimed status-push version, keyed repoID:sha
	workspaces map[int64]*store.Workspace
	users      map[int64]*store.User
	sessions   map[string]*store.Session // keyed by token hash
	members    map[int64]map[int64]bool  // userID -> set of workspace IDs
}

// New returns an empty in-memory store.
func New() *Store {
	return &Store{
		repos:      map[int64]*store.Repo{},
		uploads:    map[int64]*store.Upload{},
		files:      map[int64][]*store.UploadFile{},
		reports:    map[int64]*store.CommitReport{},
		crLocks:    map[string]*sync.Mutex{},
		crStatus:   map[string]int64{},
		workspaces: map[int64]*store.Workspace{},
		users:      map[int64]*store.User{},
		sessions:   map[string]*store.Session{},
		members:    map[int64]map[int64]bool{},
	}
}

func (s *Store) CreateRepo(_ context.Context, r *store.Repo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Mirror the Postgres UNIQUE constraints; autoCreateRepo's concurrent
	// registration fallback relies on duplicate slugs failing.
	for _, existing := range s.repos {
		if existing.Slug == r.Slug || existing.Token == r.Token {
			return fmt.Errorf("memory: repo slug or token already exists")
		}
	}
	s.repoSeq++
	r.ID = s.repoSeq
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	cp := *r
	s.repos[r.ID] = &cp
	return nil
}

func (s *Store) UpdateRepo(_ context.Context, r *store.Repo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.repos[r.ID]
	if !ok {
		return store.ErrNotFound
	}
	cp := *r
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = existing.CreatedAt
	}
	s.repos[r.ID] = &cp
	return nil
}

func (s *Store) DeleteRepo(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.repos[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.repos, id)
	for uid, u := range s.uploads {
		if u.RepoID == id {
			delete(s.uploads, uid)
			delete(s.files, uid)
		}
	}
	return nil
}

func (s *Store) RepoByID(_ context.Context, id int64) (*store.Repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.repos[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s *Store) RepoBySlug(_ context.Context, slug string) (*store.Repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		if r.Slug == slug {
			cp := *r
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *Store) RepoByToken(_ context.Context, token string) (*store.Repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		if r.Token == token {
			cp := *r
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *Store) ListRepos(_ context.Context) ([]*store.Repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*store.Repo, 0, len(s.repos))
	for _, r := range s.repos {
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func (s *Store) CreateWorkspace(_ context.Context, w *store.Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createWorkspaceLocked(w)
}

func (s *Store) createWorkspaceLocked(w *store.Workspace) error {
	for _, existing := range s.workspaces {
		if existing.Prefix == w.Prefix || existing.Token == w.Token {
			return fmt.Errorf("memory: workspace prefix or token already exists")
		}
	}
	s.wsSeq++
	w.ID = s.wsSeq
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now()
	}
	cp := *w
	s.workspaces[w.ID] = &cp
	return nil
}

func (s *Store) RegisterWorkspace(_ context.Context, w *store.Workspace, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[userID]; !ok {
		return fmt.Errorf("memory: user %d does not exist", userID)
	}
	if err := s.createWorkspaceLocked(w); err != nil {
		return err
	}
	if s.members[userID] == nil {
		s.members[userID] = map[int64]bool{}
	}
	s.members[userID][w.ID] = true
	return nil
}

func (s *Store) UpdateWorkspace(_ context.Context, w *store.Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.workspaces[w.ID]
	if !ok {
		return store.ErrNotFound
	}
	cp := *w
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = existing.CreatedAt
	}
	// Mirror postgres: the Bitbucket grant columns belong exclusively to
	// SetWorkspaceBitbucketGrant — the token rotates on every use, and a
	// full-row write from an earlier read would resurrect a dead one.
	cp.BitbucketGrantAccount = existing.BitbucketGrantAccount
	cp.BitbucketRefreshToken = existing.BitbucketRefreshToken
	cp.BitbucketGrantBroken = existing.BitbucketGrantBroken
	s.workspaces[w.ID] = &cp
	return nil
}

func (s *Store) SetWorkspaceBitbucketGrant(_ context.Context, workspaceID int64, account, refreshToken string, broken bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workspaces[workspaceID]
	if !ok {
		return store.ErrNotFound
	}
	w.BitbucketGrantAccount = account
	w.BitbucketRefreshToken = refreshToken
	w.BitbucketGrantBroken = broken
	return nil
}

func (s *Store) DeleteWorkspace(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workspaces[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.workspaces, id)
	for _, ws := range s.members {
		delete(ws, id)
	}
	return nil
}

func (s *Store) WorkspaceByPrefix(_ context.Context, prefix string) (*store.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.workspaces {
		if w.Prefix == prefix {
			cp := *w
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *Store) WorkspaceByToken(_ context.Context, token string) (*store.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.workspaces {
		if w.Token == token {
			cp := *w
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *Store) ListWorkspaces(_ context.Context) ([]*store.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*store.Workspace, 0, len(s.workspaces))
	for _, w := range s.workspaces {
		cp := *w
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out, nil
}

func (s *Store) SetUserWorkspaces(_ context.Context, userID int64, workspaceIDs []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(workspaceIDs) == 0 {
		delete(s.members, userID)
		return nil
	}
	set := make(map[int64]bool, len(workspaceIDs))
	for _, id := range workspaceIDs {
		set[id] = true
	}
	s.members[userID] = set
	return nil
}

func (s *Store) ListWorkspacesForUser(_ context.Context, userID int64) ([]*store.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*store.Workspace, 0, len(s.members[userID]))
	for wsID := range s.members[userID] {
		if w, ok := s.workspaces[wsID]; ok {
			cp := *w
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out, nil
}

func (s *Store) UpsertUser(_ context.Context, u *store.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, existing := range s.users {
		if existing.Forge == u.Forge && existing.ForgeUUID == u.ForgeUUID {
			existing.Email = u.Email
			existing.DisplayName = u.DisplayName
			existing.ForgeWorkspaces = u.ForgeWorkspaces
			existing.LastLoginAt = now
			*u = *existing
			return nil
		}
	}
	s.userSeq++
	u.ID = s.userSeq
	u.CreatedAt = now
	u.LastLoginAt = now
	cp := *u
	s.users[u.ID] = &cp
	return nil
}

func (s *Store) UserByID(_ context.Context, id int64) (*store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (s *Store) ListUsers(_ context.Context) ([]*store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*store.User, 0, len(s.users))
	for _, u := range s.users {
		cp := *u
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) DeleteUser(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.users, id)
	delete(s.members, id)
	for hash, sess := range s.sessions {
		if sess.UserID == id {
			delete(s.sessions, hash)
		}
	}
	return nil
}

func (s *Store) CreateSession(_ context.Context, sess *store.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[sess.UserID]; !ok {
		return fmt.Errorf("memory: session user %d does not exist", sess.UserID)
	}
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	cp := *sess
	s.sessions[sess.TokenHash] = &cp
	return nil
}

func (s *Store) UserBySession(_ context.Context, tokenHash string) (*store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[tokenHash]
	if !ok {
		return nil, store.ErrNotFound
	}
	if !sess.ExpiresAt.After(time.Now()) {
		delete(s.sessions, tokenHash) // lazy cleanup, matching postgres
		return nil, store.ErrNotFound
	}
	u, ok := s.users[sess.UserID]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (s *Store) DeleteSession(_ context.Context, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[tokenHash]; !ok {
		return store.ErrNotFound
	}
	delete(s.sessions, tokenHash)
	return nil
}

func (s *Store) CreateUpload(_ context.Context, u *store.Upload, files []*store.UploadFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upSeq++
	u.ID = s.upSeq
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	cp := copyUpload(u)
	s.uploads[u.ID] = cp
	fs := make([]*store.UploadFile, 0, len(files))
	for _, f := range files {
		fcp := *f
		fcp.UploadID = u.ID
		fs = append(fs, &fcp)
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].Path < fs[j].Path })
	s.files[u.ID] = fs
	return nil
}

func (s *Store) Upload(_ context.Context, id int64) (*store.Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.uploads[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return copyUpload(u), nil
}

// copyUpload deep-copies an upload so callers and the store never alias the
// same DiffCoverage, matching the postgres JSON round-trip semantics.
func copyUpload(u *store.Upload) *store.Upload {
	cp := *u
	cp.DiffCoverage = u.DiffCoverage.Clone()
	return &cp
}

func (s *Store) ListUploads(_ context.Context, repoID int64, limit int) ([]*store.Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*store.Upload
	for _, u := range s.uploads {
		if u.RepoID == repoID {
			out = append(out, copyUpload(u))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) UploadFiles(_ context.Context, uploadID int64) ([]*store.UploadFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fs, ok := s.files[uploadID]
	if !ok {
		return nil, store.ErrNotFound
	}
	out := make([]*store.UploadFile, 0, len(fs))
	for _, f := range fs {
		cp := *f
		out = append(out, &cp)
	}
	return out, nil
}

func (s *Store) ListBranchUploads(_ context.Context, repoID int64, branch string, limit int) ([]*store.Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*store.Upload
	for _, u := range s.uploads {
		if u.RepoID == repoID && u.Branch == branch {
			out = append(out, copyUpload(u))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) LatestUploadsPerPart(_ context.Context, repoID int64, commitSHA string) ([]*store.Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	latest := map[string]*store.Upload{}
	for _, u := range s.uploads {
		if u.RepoID != repoID || u.CommitSHA != commitSHA {
			continue
		}
		if cur, ok := latest[u.Part]; !ok || u.ID > cur.ID {
			latest[u.Part] = u
		}
	}
	out := make([]*store.Upload, 0, len(latest))
	for _, u := range latest {
		out = append(out, copyUpload(u))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) WithCommitReportTx(ctx context.Context, repoID int64, commitSHA string, fn func(context.Context, store.CommitTx) error) error {
	key := fmt.Sprintf("%d:%s", repoID, commitSHA)
	s.mu.Lock()
	m := s.crLocks[key]
	if m == nil {
		m = &sync.Mutex{}
		s.crLocks[key] = m
	}
	s.mu.Unlock()
	m.Lock()
	defer m.Unlock()
	// The store's own methods satisfy store.CommitTx; the per-commit mutex
	// gives the same serialization the Postgres advisory lock does.
	return fn(ctx, s)
}

func (s *Store) UpsertCommitReport(_ context.Context, cr *store.CommitReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, existing := range s.reports {
		if existing.RepoID == cr.RepoID && existing.CommitSHA == cr.CommitSHA {
			// Preserve id and first-seen created_at across recomputes.
			cr.ID = existing.ID
			cr.CreatedAt = existing.CreatedAt
			cr.UpdatedAt = now
			s.reports[cr.ID] = copyCommitReport(cr)
			return nil
		}
	}
	s.crSeq++
	cr.ID = s.crSeq
	if cr.CreatedAt.IsZero() {
		cr.CreatedAt = now
	}
	cr.UpdatedAt = now
	s.reports[cr.ID] = copyCommitReport(cr)
	return nil
}

func (s *Store) CommitReport(_ context.Context, repoID int64, commitSHA string) (*store.CommitReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cr := range s.reports {
		if cr.RepoID == repoID && cr.CommitSHA == commitSHA {
			return copyCommitReport(cr), nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *Store) LatestCommitReport(_ context.Context, repoID int64, branch string) (*store.CommitReport, error) {
	return s.latestCommitReport(repoID, branch, "", false)
}

func (s *Store) LatestPassedCommitReport(_ context.Context, repoID int64, branch, excludeCommit string) (*store.CommitReport, error) {
	return s.latestCommitReport(repoID, branch, excludeCommit, true)
}

func (s *Store) latestCommitReport(repoID int64, branch, excludeCommit string, passedOnly bool) (*store.CommitReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest *store.CommitReport
	for _, cr := range s.reports {
		if cr.RepoID != repoID || cr.Branch != branch {
			continue
		}
		if passedOnly && cr.GateFailed {
			continue
		}
		if excludeCommit != "" && cr.CommitSHA == excludeCommit {
			continue
		}
		if latest == nil || cr.ID > latest.ID {
			latest = cr
		}
	}
	if latest == nil {
		return nil, store.ErrNotFound
	}
	return copyCommitReport(latest), nil
}

func (s *Store) ClaimStatusPush(_ context.Context, repoID int64, commitSHA string, version int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Mirror the Postgres UPDATE, which claims nothing when the report row
	// does not exist yet.
	exists := false
	for _, cr := range s.reports {
		if cr.RepoID == repoID && cr.CommitSHA == commitSHA {
			exists = true
			break
		}
	}
	if !exists {
		return false, nil
	}
	key := fmt.Sprintf("%d:%s", repoID, commitSHA)
	if s.crStatus[key] < version {
		s.crStatus[key] = version
		return true, nil
	}
	return false, nil
}

func (s *Store) ListBranchCommitReports(_ context.Context, repoID int64, branch string, limit int) ([]*store.CommitReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*store.CommitReport
	for _, cr := range s.reports {
		if cr.RepoID == repoID && cr.Branch == branch {
			out = append(out, copyCommitReport(cr))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// copyCommitReport deep-copies a report so callers never alias the stored
// DiffCoverage, matching the Postgres JSON round-trip.
func copyCommitReport(cr *store.CommitReport) *store.CommitReport {
	cp := *cr
	cp.DiffCoverage = cr.DiffCoverage.Clone()
	return &cp
}
