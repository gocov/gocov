package core

import "testing"

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
