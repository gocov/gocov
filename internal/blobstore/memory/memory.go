// Package memory provides an in-memory blobstore.Store for tests and the
// gocov-preview harness.
package memory

import (
	"bytes"
	"context"
	"sync"

	"github.com/gocov/gocov/internal/blobstore"
)

// Store is an in-memory blobstore. Safe for concurrent use.
type Store struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

// New returns an empty in-memory blobstore.
func New() *Store {
	return &Store{blobs: map[string][]byte{}}
}

func (s *Store) Put(_ context.Context, key string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blobs[key] = append([]byte{}, data...)
	return nil
}

func (s *Store) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.blobs[key]
	if !ok {
		return nil, blobstore.ErrNotFound
	}
	return bytes.Clone(data), nil
}

func (s *Store) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.blobs, key)
	return nil
}
