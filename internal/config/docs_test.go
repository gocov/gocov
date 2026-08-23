package config

import (
	"os"
	"regexp"
	"strings"
	"testing"

	env "github.com/caarlos0/env/v11"
)

// configurationDoc is the user-facing variable table that mirrors the
// Server struct.
const configurationDoc = "../../docs/configuration.md"

// docRow matches one row of that table: | `NAME` | default | description |
var docRow = regexp.MustCompile("^\\|\\s*`([A-Z][A-Z0-9_]*)`\\s*\\|")

// TestConfigurationDocIsInSync makes "the struct is the authoritative
// list" an enforced claim rather than a hopeful one: a variable added to
// Server without a row in docs/configuration.md — or a row left behind by
// a variable that was removed — fails here instead of silently misleading
// self-hosters.
//
// Only Server is covered. The CLI's variables are documented in prose
// across ci-upload.md, parts.md and getting-started.md rather than in a
// table, and GOCOV_UPLOADER_KIND is set by the gocov-action rather than
// by users, so there is no table to hold them to. Preview is a dev
// harness and deliberately undocumented.
func TestConfigurationDocIsInSync(t *testing.T) {
	fields, err := env.GetFieldParams(&Server{})
	if err != nil {
		t.Fatalf("GetFieldParams: %v", err)
	}
	if len(fields) == 0 {
		t.Fatal("GetFieldParams returned nothing; the struct tags are not being read")
	}

	doc, err := os.ReadFile(configurationDoc)
	if err != nil {
		t.Fatalf("reading %s: %v", configurationDoc, err)
	}
	// Each documented variable mapped to the rest of its row.
	documented := map[string]string{}
	for _, line := range strings.Split(string(doc), "\n") {
		if m := docRow.FindStringSubmatch(line); m != nil {
			documented[m[1]] = line
		}
	}

	declared := map[string]bool{}
	for _, field := range fields {
		declared[field.Key] = true
		row, ok := documented[field.Key]
		if !ok {
			t.Errorf("%s is declared in config.Server but has no row in %s", field.Key, configurationDoc)
			continue
		}
		// A default that disagrees with the documented one is the drift
		// that bites hardest: nothing fails, the docs are just wrong.
		if field.HasDefaultValue && !strings.Contains(row, field.DefaultValue) {
			t.Errorf("%s defaults to %q in code, but its row in %s does not say so:\n%s",
				field.Key, field.DefaultValue, configurationDoc, row)
		}
		if (field.Required || field.NotEmpty) && !strings.Contains(strings.ToLower(row), "required") {
			t.Errorf("%s is required in code, but its row in %s does not say so:\n%s",
				field.Key, configurationDoc, row)
		}
	}
	for name := range documented {
		if !declared[name] {
			t.Errorf("%s is documented in %s but no longer read by config.Server", name, configurationDoc)
		}
	}
}
