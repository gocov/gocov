package store

import "testing"

func TestJudgedGate(t *testing.T) {
	current := Gate{MinCoverage: new(80.0)}
	recorded := Gate{MaxCoverageDrop: new(0.0)}

	if got := JudgedGate(&recorded, current); got != recorded {
		t.Errorf("JudgedGate(recorded, current) = %+v, want the recorded gate %+v", got, recorded)
	}
	if got := JudgedGate(nil, current); got != current {
		t.Errorf("JudgedGate(nil, current) = %+v, want the current gate %+v", got, current)
	}
}

func TestGateConfigured(t *testing.T) {
	tests := []struct {
		name string
		gate Gate
		want bool
	}{
		{"no rules", Gate{}, false},
		{"min coverage", Gate{MinCoverage: new(80.0)}, true},
		{"min diff coverage", Gate{MinDiffCoverage: new(90.0)}, true},
		{"zero max drop is still a rule", Gate{MaxCoverageDrop: new(0.0)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gate.Configured(); got != tt.want {
				t.Errorf("Configured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRepoReportsPublic(t *testing.T) {
	tests := []struct {
		name string
		repo Repo
		want bool
	}{
		{"public", Repo{Visibility: VisibilityPublic}, true},
		{"public with reports turned off", Repo{Visibility: VisibilityPublic, PublicReportsDisabled: true}, false},
		{"private", Repo{Visibility: VisibilityPrivate}, false},
		{"visibility never checked", Repo{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.repo.ReportsPublic(); got != tt.want {
				t.Errorf("ReportsPublic() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkspaceOwns(t *testing.T) {
	ws := &Workspace{Forge: "github", Prefix: "acme"}
	tests := []struct {
		name string
		repo Repo
		want bool
	}{
		{"repo under the prefix", Repo{Forge: "github", Slug: "acme/api"}, true},
		{"other forge", Repo{Forge: "gitlab", Slug: "acme/api"}, false},
		{"other workspace", Repo{Forge: "github", Slug: "other/api"}, false},
		{"prefix is only a string prefix", Repo{Forge: "github", Slug: "acme-labs/api"}, false},
		{"bare prefix", Repo{Forge: "github", Slug: "acme"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ws.Owns(&tt.repo); got != tt.want {
				t.Errorf("Owns(%s/%s) = %v, want %v", tt.repo.Forge, tt.repo.Slug, got, tt.want)
			}
		})
	}
}
