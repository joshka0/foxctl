package daemon

import (
	"context"
	"sync"
)

// DedupeStore tracks processed message IDs to prevent duplicate work.
type DedupeStore interface {
	IsProcessed(ctx context.Context, agentID, messageID string) (bool, error)
	MarkProcessed(ctx context.Context, agentID, messageID string) error
}

// MemoryDedupeStore is an in-memory implementation of DedupeStore.
type MemoryDedupeStore struct {
	mu        sync.Mutex
	processed map[string]bool // key: agentID:messageID
}

// NewMemoryDedupeStore creates a new in-memory dedupe store.
func NewMemoryDedupeStore() *MemoryDedupeStore {
	return &MemoryDedupeStore{
		processed: make(map[string]bool),
	}
}

// IsProcessed checks if a message has already been processed.
func (s *MemoryDedupeStore) IsProcessed(ctx context.Context, agentID, messageID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := agentID + ":" + messageID
	return s.processed[key], nil
}

// MarkProcessed marks a message as processed.
func (s *MemoryDedupeStore) MarkProcessed(ctx context.Context, agentID, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := agentID + ":" + messageID
	s.processed[key] = true
	return nil
}
