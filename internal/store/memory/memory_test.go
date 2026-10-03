package memory

import (
	"testing"

	"github.com/gocov/gocov/internal/store"
	"github.com/gocov/gocov/internal/store/storetest"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(*testing.T) store.Store { return New() })
}
