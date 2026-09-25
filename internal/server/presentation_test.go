package server

import (
	"encoding/json"
	"maps"
	"os"
	"testing"
)

// presentationRules is testdata/presentation.json: the presentation rules
// the server and the web app both apply. web/src/lib/presentation.test.ts
// checks the app against the same file.
type presentationRules struct {
	CoverageLevels []struct {
		Pct   float64 `json:"pct"`
		Level string  `json:"level"`
	} `json:"coverage_levels"`
	Deltas []struct {
		Delta float64 `json:"delta"`
		Moved bool    `json:"moved"`
	} `json:"deltas"`
	ForgeNames map[string]string `json:"forge_names"`
	AppAccount string            `json:"app_account"`
}

// The badge colours, the files card's "coverage changed", the forge names
// in OIDC refusals and the app's posting identity say what the web app
// says: coverage levels, trend arrows, forge labels, the Reporting card.
func TestPresentationRulesMatchTheApp(t *testing.T) {
	raw, err := os.ReadFile("testdata/presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	var rules presentationRules
	if err := json.Unmarshal(raw, &rules); err != nil {
		t.Fatal(err)
	}

	colors := map[string]string{"bad": badgeRed, "warn": badgeYellow, "good": badgeGreen}
	for _, c := range rules.CoverageLevels {
		if got := badgeColor(c.Pct); got != colors[c.Level] {
			t.Errorf("badgeColor(%v) = %s, want the %s colour %s", c.Pct, got, c.Level, colors[c.Level])
		}
	}
	for _, d := range rules.Deltas {
		if got := coverageMoved(d.Delta); got != d.Moved {
			t.Errorf("coverageMoved(%v) = %v, want %v", d.Delta, got, d.Moved)
		}
	}
	if !maps.Equal(oidcForgeNames, rules.ForgeNames) {
		t.Errorf("oidcForgeNames = %v, want %v", oidcForgeNames, rules.ForgeNames)
	}
	if appAccount != rules.AppAccount {
		t.Errorf("appAccount = %q, want %q", appAccount, rules.AppAccount)
	}
}
