package enrichers

import (
	"fmt"
	"strings"
	"sync"

	"github.com/joshka0/foxctl/internal/v2/core/run"
)

const (
	defaultQueueBuffer = 128
)

// Job is one asynchronous enrichment request for a persisted turn.
type Job struct {
	TurnID          string
	ArtifactType    string
	ArtifactVersion string
	Turn            run.TurnRecord
}

// Key returns the idempotency key used for enqueue dedupe.
func (j Job) Key() string {
	turnID := strings.TrimSpace(j.TurnID)
	if turnID == "" {
		turnID = strings.TrimSpace(j.Turn.ID)
	}
	return fmt.Sprintf(
		"%s|%s|%s",
		turnID,
		strings.TrimSpace(j.ArtifactType),
		strings.TrimSpace(j.ArtifactVersion),
	)
}

// Queue is a bounded, non-blocking enrichment queue with in-flight key dedupe.
type Queue struct {
	mu     sync.Mutex
	ch     chan Job
	seen   map[string]struct{}
	closed bool
}

// NewQueue creates a bounded queue.
func NewQueue(buffer int) *Queue {
	if buffer <= 0 {
		buffer = defaultQueueBuffer
	}
	return &Queue{
		ch:   make(chan Job, buffer),
		seen: make(map[string]struct{}),
	}
}

// Enqueue attempts to enqueue a job without blocking.
// It returns true when accepted and false when deduped, invalid, closed, or full.
func (q *Queue) Enqueue(job Job) bool {
	if q == nil {
		return false
	}
	key := strings.TrimSpace(job.Key())
	if key == "||" || key == "" {
		return false
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return false
	}
	if _, exists := q.seen[key]; exists {
		return false
	}

	// Keep close/send mutually exclusive by holding q.mu while selecting on q.ch.
	select {
	case q.ch <- job:
		q.seen[key] = struct{}{}
		return true
	default:
		return false
	}
}

// Jobs returns a read-only channel of queued enrichment jobs.
func (q *Queue) Jobs() <-chan Job {
	if q == nil {
		return nil
	}
	return q.ch
}

// Release clears one dedupe key after worker completion.
func (q *Queue) Release(job Job) {
	if q == nil {
		return
	}
	key := strings.TrimSpace(job.Key())
	if key == "||" || key == "" {
		return
	}

	q.mu.Lock()
	delete(q.seen, key)
	q.mu.Unlock()
}

// Close closes the queue exactly once.
func (q *Queue) Close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	close(q.ch)
	q.mu.Unlock()
}
