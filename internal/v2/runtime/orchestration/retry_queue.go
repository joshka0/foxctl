package orchestration

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultRetryQueueCapacity = 1024

// RetryEntry tracks one pending retry candidate.
type RetryEntry struct {
	Candidate Candidate
	Attempt   int
	DueAt     time.Time
	LastError string
}

// RetryQueue is a bounded in-memory retry queue keyed by issue id.
type RetryQueue struct {
	mu       sync.Mutex
	capacity int
	byIssue  map[string]RetryEntry
}

// NewRetryQueue creates a retry queue with bounded capacity.
func NewRetryQueue(capacity int) *RetryQueue {
	if capacity <= 0 {
		capacity = defaultRetryQueueCapacity
	}
	return &RetryQueue{
		capacity: capacity,
		byIssue:  map[string]RetryEntry{},
	}
}

// Upsert inserts or replaces one retry entry for the candidate issue id.
// Returns false when queue is at capacity and the issue key does not already exist.
func (q *RetryQueue) Upsert(entry RetryEntry) bool {
	if q == nil {
		return false
	}
	key := strings.TrimSpace(entry.Candidate.IssueID)
	if key == "" {
		return false
	}
	if entry.Attempt < 1 {
		entry.Attempt = 1
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if _, exists := q.byIssue[key]; !exists && len(q.byIssue) >= q.capacity {
		return false
	}
	q.byIssue[key] = entry
	return true
}

// PopDue removes and returns due entries sorted by (due_at, issue_id).
func (q *RetryQueue) PopDue(now time.Time, limit int) []RetryEntry {
	if q == nil {
		return nil
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if limit <= 0 {
		limit = len(q.byIssue)
	}

	due := make([]RetryEntry, 0, len(q.byIssue))
	for _, entry := range q.byIssue {
		if entry.DueAt.IsZero() || !entry.DueAt.After(now) {
			due = append(due, entry)
		}
	}

	sort.SliceStable(due, func(i, j int) bool {
		if due[i].DueAt.Equal(due[j].DueAt) {
			return strings.TrimSpace(due[i].Candidate.IssueID) < strings.TrimSpace(due[j].Candidate.IssueID)
		}
		return due[i].DueAt.Before(due[j].DueAt)
	})

	if len(due) > limit {
		due = due[:limit]
	}

	for _, entry := range due {
		delete(q.byIssue, strings.TrimSpace(entry.Candidate.IssueID))
	}
	return due
}

// Len returns queue size.
func (q *RetryQueue) Len() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.byIssue)
}
