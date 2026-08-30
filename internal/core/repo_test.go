package core

import (
	"context"
	"errors"
	"testing"

	"github.com/gocov/gocov/internal/forge"
	forgefake "github.com/gocov/gocov/internal/forge/fake"
	"github.com/gocov/gocov/internal/store"
)

func TestValidRepoName(t *testing.T) {
	tests := []struct {
		forge, name string
		want        bool
	}{
		{"bitbucket", "widgets", true},
		{"bitbucket", "sub/widgets", false},
		{"github", "sub/widgets", false},
		{"gitlab", "widgets", true},
		{"gitlab", "sub/widgets", true},
		{"gitlab", "sub/team/widgets", true},
		{"gitlab", "sub//widgets", false},
		{"gitlab", "sub/../widgets", false},
		{"gitlab", "", false},
	}
	for _, tt := range tests {
		if got := ValidRepoName(tt.forge, tt.name); got != tt.want {
			t.Errorf("ValidRepoName(%q, %q) = %v, want %v", tt.forge, tt.name, got, tt.want)
		}
	}
}

func TestRefreshVisibilityCachesForgeAnswer(t *testing.T) {
	p, st, repo := newPipeline(t, store.Gate{})
	ctx := context.Background()

	fg := forgefake.New()
	fg.Visibility = forge.VisibilityPublic
	p.RefreshVisibility(ctx, fg, repo)
	if repo.Visibility != store.VisibilityPublic {
		t.Errorf("repo.Visibility = %q, want public", repo.Visibility)
	}
	stored, err := st.RepoBySlug(ctx, repo.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Visibility != store.VisibilityPublic {
		t.Errorf("stored visibility = %q, want public", stored.Visibility)
	}

	// Flipped private on the forge: the cache follows.
	fg.Visibility = forge.VisibilityPrivate
	p.RefreshVisibility(ctx, fg, repo)
	if stored, _ := st.RepoBySlug(ctx, repo.Slug); stored.Visibility != store.VisibilityPrivate {
		t.Errorf("stored visibility after flip = %q, want private", stored.Visibility)
	}

	// A forge that cannot answer keeps the last known state; so does
	// having no forge at all.
	fg.VisibilityErr = errors.New("forge down")
	p.RefreshVisibility(ctx, fg, repo)
	p.RefreshVisibility(ctx, nil, repo)
	if stored, _ := st.RepoBySlug(ctx, repo.Slug); stored.Visibility != store.VisibilityPrivate {
		t.Errorf("stored visibility after failures = %q, want private", stored.Visibility)
	}
}
