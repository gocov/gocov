package main

import (
	"os"
	"strings"
	"testing"
)

// TestMain runs the tests outside any CI. The tests that drive run() read
// the real environment, and on a CI runner its own variables leak in: a
// GitHub runner's GITHUB_ACTIONS outranks the Bitbucket and GitLab
// variables a test sets, and its GITHUB_EVENT_PATH swaps in the PR's head
// SHA, which the runner's shallow merge checkout cannot show. Which paths
// the tests took — and so the CLI's coverage — depended on the event that
// ran CI. Each test sets the variables it means.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		for _, prefix := range []string{"GITHUB_", "ACTIONS_", "BITBUCKET_", "GITLAB_", "CI_", "GOCOV_"} {
			if strings.HasPrefix(name, prefix) {
				os.Unsetenv(name)
			}
		}
	}
	os.Exit(m.Run())
}
