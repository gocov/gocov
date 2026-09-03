package main

import (
	"flag"
	"io"
	"slices"
	"testing"

	"github.com/gocov/gocov/internal/ignore"
)

// -ignore starts from $GOCOV_IGNORE and is repeatable; the first flag on
// the command line replaces the environment's list rather than extending
// it, so flags win over the environment like everywhere else in the CLI.
func TestIgnoreFlagReplacesEnvironment(t *testing.T) {
	newSet := func() (*flag.FlagSet, *patternList) {
		fs := flag.NewFlagSet("upload", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		l := &patternList{patterns: ignore.Parse("env/**,*_env.go")}
		fs.Var(l, "ignore", "")
		return fs, l
	}

	fs, l := newSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"env/**", "*_env.go"}; !slices.Equal(l.patterns, want) {
		t.Errorf("no flag: %q, want the environment's %q", l.patterns, want)
	}

	fs, l = newSet()
	if err := fs.Parse([]string{"-ignore", "cmd/preview/**", "-ignore", "gen/*,*_mock.go"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"cmd/preview/**", "gen/*", "*_mock.go"}; !slices.Equal(l.patterns, want) {
		t.Errorf("flags: %q, want %q", l.patterns, want)
	}
}
