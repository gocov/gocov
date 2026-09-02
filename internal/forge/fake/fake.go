// Package fake provides a recording forge.Forge test double.
package fake

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/gocov/gocov/internal/forge"
)

// StatusCall records one PostBuildStatus invocation.
type StatusCall struct {
	RepoSlug  string
	CommitSHA string
	Status    forge.BuildStatus
}

// CommentCall records one PostPRComment invocation.
type CommentCall struct {
	RepoSlug string
	PRID     string
	Body     string
}

// UpdateCall records one UpdatePRComment invocation.
type UpdateCall struct {
	RepoSlug  string
	PRID      string
	CommentID string
	Body      string
}

// ReportCall records one PublishReport invocation.
type ReportCall struct {
	RepoSlug    string
	CommitSHA   string
	Report      forge.Report
	Annotations []forge.Annotation
}

// Forge records calls and returns configurable errors.
type Forge struct {
	mu sync.Mutex

	StatusErr  error  // returned by PostBuildStatus
	CommentErr error  // returned by PostPRComment
	FindErr    error  // returned by FindPRComment
	UpdateErr  error  // returned by UpdatePRComment
	DiffText   string // returned by GetPRDiff; empty means ErrNotImplemented
	DiffErr    error  // returned by GetPRDiff when set
	// DefaultBranch is returned by GetDefaultBranch; empty means
	// ErrNotImplemented. DefaultBranchErr wins when set.
	DefaultBranch    string
	DefaultBranchErr error
	// Files maps paths to contents for GetFileContent; missing paths
	// yield ErrRepoNotFound. FileErr wins when set.
	Files     map[string]string
	FileErr   error
	ReportErr error // returned by PublishReport
	// Visibility is returned by GetRepoVisibility; empty means
	// ErrNotImplemented. VisibilityErr wins when set.
	Visibility    string
	VisibilityErr error

	// RepoID is returned by GetRepoID; empty means ErrNotImplemented.
	// RepoIDErr wins when set.
	RepoID    string
	RepoIDErr error

	StatusCalls        []StatusCall
	CommentCalls       []CommentCall
	UpdateCalls        []UpdateCall
	FindCalls          []string // prefixes
	DiffCalls          []DiffCall
	DefaultBranchCalls []string // repo slugs
	FileCalls          []string // paths
	ReportCalls        []ReportCall
	VisibilityCalls    []string // repo slugs
	RepoIDCalls        []string // repo slugs

	// comments simulates the PR comment store: posted and updated bodies
	// keyed by a fake incremental id, so FindPRComment behaves like the
	// real forge across multiple uploads.
	comments []string
}

// DiffCall records one GetPRDiff invocation.
type DiffCall struct {
	RepoSlug string
	PRID     string
}

// New returns an empty fake forge.
func New() *Forge { return &Forge{} }

func (f *Forge) PostBuildStatus(_ context.Context, repoSlug, commitSHA string, status forge.BuildStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.StatusErr != nil {
		return f.StatusErr
	}
	f.StatusCalls = append(f.StatusCalls, StatusCall{repoSlug, commitSHA, status})
	return nil
}

func (f *Forge) PostPRComment(_ context.Context, repoSlug, prID, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CommentErr != nil {
		return f.CommentErr
	}
	f.CommentCalls = append(f.CommentCalls, CommentCall{repoSlug, prID, body})
	f.comments = append(f.comments, body)
	return nil
}

func (f *Forge) FindPRComment(_ context.Context, repoSlug, prID, prefix string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.FindCalls = append(f.FindCalls, prefix)
	if f.FindErr != nil {
		return "", f.FindErr
	}
	for i, v := range slices.Backward(f.comments) {
		if strings.HasPrefix(v, prefix) {
			return strconv.Itoa(i), nil
		}
	}
	return "", nil
}

func (f *Forge) UpdatePRComment(_ context.Context, repoSlug, prID, commentID, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UpdateErr != nil {
		return f.UpdateErr
	}
	i, err := strconv.Atoi(commentID)
	if err != nil || i < 0 || i >= len(f.comments) {
		return fmt.Errorf("fake: unknown comment id %q", commentID)
	}
	f.comments[i] = body
	f.UpdateCalls = append(f.UpdateCalls, UpdateCall{repoSlug, prID, commentID, body})
	return nil
}

func (f *Forge) GetDefaultBranch(_ context.Context, repoSlug string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DefaultBranchCalls = append(f.DefaultBranchCalls, repoSlug)
	if f.DefaultBranchErr != nil {
		return "", f.DefaultBranchErr
	}
	if f.DefaultBranch == "" {
		return "", forge.ErrNotImplemented
	}
	return f.DefaultBranch, nil
}

func (f *Forge) GetRepoVisibility(_ context.Context, repoSlug string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.VisibilityCalls = append(f.VisibilityCalls, repoSlug)
	if f.VisibilityErr != nil {
		return "", f.VisibilityErr
	}
	if f.Visibility == "" {
		return "", forge.ErrNotImplemented
	}
	return f.Visibility, nil
}

func (f *Forge) GetRepoID(_ context.Context, repoSlug string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RepoIDCalls = append(f.RepoIDCalls, repoSlug)
	if f.RepoIDErr != nil {
		return "", f.RepoIDErr
	}
	if f.RepoID == "" {
		return "", forge.ErrNotImplemented
	}
	return f.RepoID, nil
}

func (f *Forge) GetFileContent(_ context.Context, repoSlug, commitSHA, path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.FileCalls = append(f.FileCalls, path)
	if f.FileErr != nil {
		return nil, f.FileErr
	}
	content, ok := f.Files[path]
	if !ok {
		return nil, fmt.Errorf("%w: %s at %s", forge.ErrRepoNotFound, path, commitSHA)
	}
	return []byte(content), nil
}

func (f *Forge) GetPRDiff(_ context.Context, repoSlug, prID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DiffCalls = append(f.DiffCalls, DiffCall{repoSlug, prID})
	if f.DiffErr != nil {
		return "", f.DiffErr
	}
	if f.DiffText == "" {
		return "", forge.ErrNotImplemented
	}
	return f.DiffText, nil
}

func (f *Forge) PublishReport(_ context.Context, repoSlug, commitSHA string, report forge.Report, annotations []forge.Annotation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ReportErr != nil {
		return f.ReportErr
	}
	f.ReportCalls = append(f.ReportCalls, ReportCall{repoSlug, commitSHA, report, annotations})
	return nil
}

var _ forge.Forge = (*Forge)(nil)
