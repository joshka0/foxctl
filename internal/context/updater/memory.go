package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// ShortTermMemory tracks recently injected contexts to avoid repetition.
// It uses a ring buffer with TTL-based expiration.
type ShortTermMemory struct {
	mu       sync.RWMutex
	records  []InjectionRecord
	capacity int
	ttl      time.Duration
	index    int // next write position
}

// NewShortTermMemory creates a new short-term memory with the given capacity and TTL.
func NewShortTermMemory(capacity int, ttl time.Duration) *ShortTermMemory {
	if capacity < 1 {
		capacity = 50
	}
	if ttl < time.Minute {
		ttl = 30 * time.Minute
	}
	return &ShortTermMemory{
		records:  make([]InjectionRecord, 0, capacity),
		capacity: capacity,
		ttl:      ttl,
	}
}

// Record adds an injection record to memory.
func (m *ShortTermMemory) Record(id, content, sessionID string, topics []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := InjectionRecord{
		ID:          id,
		ContentHash: hashContent(content),
		SessionID:   sessionID,
		Timestamp:   time.Now(),
		Topics:      topics,
	}

	// Ring buffer: overwrite oldest if at capacity
	if len(m.records) < m.capacity {
		m.records = append(m.records, record)
	} else {
		m.records[m.index] = record
		m.index = (m.index + 1) % m.capacity
	}
}

// WasRecentlyInjected checks if a context was recently injected.
// It checks both by ID and by content hash.
func (m *ShortTermMemory) WasRecentlyInjected(id, content, sessionID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	contentHash := hashContent(content)
	cutoff := time.Now().Add(-m.ttl)

	for _, record := range m.records {
		// Skip expired records
		if record.Timestamp.Before(cutoff) {
			continue
		}

		// Check same session
		if record.SessionID != sessionID {
			continue
		}

		// Check by ID or content hash
		if record.ID == id || record.ContentHash == contentHash {
			return true
		}
	}

	return false
}

// RecentTopics returns the topics from recent injections (for drift detection).
func (m *ShortTermMemory) RecentTopics(sessionID string, since time.Duration) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cutoff := time.Now().Add(-since)
	topicSet := make(map[string]struct{})

	for _, record := range m.records {
		if record.Timestamp.Before(cutoff) {
			continue
		}
		if record.SessionID != sessionID {
			continue
		}
		for _, topic := range record.Topics {
			topicSet[topic] = struct{}{}
		}
	}

	topics := make([]string, 0, len(topicSet))
	for topic := range topicSet {
		topics = append(topics, topic)
	}
	return topics
}

// Count returns the number of non-expired records for a session.
func (m *ShortTermMemory) Count(sessionID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cutoff := time.Now().Add(-m.ttl)
	count := 0

	for _, record := range m.records {
		if record.Timestamp.Before(cutoff) {
			continue
		}
		if record.SessionID == sessionID {
			count++
		}
	}

	return count
}

// Prune removes expired records. Called periodically to keep memory clean.
func (m *ShortTermMemory) Prune() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-m.ttl)
	newRecords := make([]InjectionRecord, 0, len(m.records))

	for _, record := range m.records {
		if record.Timestamp.After(cutoff) {
			newRecords = append(newRecords, record)
		}
	}

	pruned := len(m.records) - len(newRecords)
	m.records = newRecords
	m.index = 0 // Reset index after pruning

	return pruned
}

// hashContent creates a SHA256 hash of content for deduplication.
func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:8]) // Use first 8 bytes (16 hex chars) for efficiency
}
