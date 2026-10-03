package storetest_test

import (
	"testing"

	"github.com/gocov/gocov/internal/store"
	"github.com/gocov/gocov/internal/store/memory"
	"github.com/gocov/gocov/internal/store/storetest"
)

// The suite runs on the memory store here too, so its own statements are
// measured where they live rather than reported as an untested package.
func TestRun(t *testing.T) {
	storetest.Run(t, func(*testing.T) store.Store { return memory.New() })
}
