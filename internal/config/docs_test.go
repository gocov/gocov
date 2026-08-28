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

// Columns of the variable table, counted the way strings.Split on "|"
// numbers them: the leading delimiter produces an empty field 0.
const (
	defaultColumn     = 2
	descriptionColumn = 3
)

// column returns one cell of a table row, stripped of the padding and the
// backticks the table wraps literal values in. The description column is
// taken as everything that follows, so a stray "|" inside prose cannot
// silently truncate it.
func column(row string, n int) string {
	cells := strings.Split(row, "|")
	if len(cells) <= n {
		return ""
	}
	cell := cells[n]
	if n == descriptionColumn {
		cell = strings.Join(cells[n:], "|")
	}
	return strings.Trim(cell, " `|")
}

// docRow matches one row of that table: | `NAME` | default | description |
var docRow = regexp.MustCompile("^\\|\\s*`([A-Z][A-Z0-9_]*)`\\s*\\|")

// TestConfigurationDocIsInSync makes "the struct is the authoritative
// list" an enforced claim rather than a hopeful one: a variable added to
// Server without a row in docs/configuration.md — or a row left behind by
// a variable that was removed — fails here instead of silently misleading
// self-hosters.
//
// Only Server is covered. The CLI's variables have their own reference
// table in docs/cli.md, but it lists flags and their env equivalents in
// prose-shaped rows rather than the NAME|default|description shape this
// test parses, and GOCOV_UPLOADER_KIND is set by the gocov-action rather
// than by users. Preview is a dev harness and deliberately undocumented.
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
	for line := range strings.SplitSeq(string(doc), "\n") {
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
		// Compare the default column exactly rather than searching the
		// row — a substring test would let ":8080" narrowed to ":80"
		// through, since the stale cell still contains the new value.
		if got := column(row, defaultColumn); field.HasDefaultValue && got != field.DefaultValue {
			t.Errorf("%s defaults to %q in code, but %s documents %q:\n%s",
				field.Key, field.DefaultValue, configurationDoc, got, row)
		}
		// One-directional on purpose. "Required in code but not in the
		// docs" is checked; the reverse is not, because the word shows up
		// in honest prose about optional variables too (the webhook
		// secret is "required for a Marketplace listing; optional
		// otherwise"), and a symmetric check would need a marker
		// convention the table does not have.
		if desc := column(row, descriptionColumn); (field.Required || field.NotEmpty) &&
			!strings.Contains(strings.ToLower(desc), "required") {
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
