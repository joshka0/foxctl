package console

import (
	"errors"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// ErrMaxInFlightReached indicates the maximum number of in-flight correlations has been reached.
var ErrMaxInFlightReached = errors.New("console: max in-flight correlations reached")

// CorrelationTracker tracks pending ask/reply correlations for a console session.
//
// It enforces backpressure by limiting the number of in-flight correlations
// (typically 1 per the design doc's "single in-flight" default).
type CorrelationTracker struct {
	mu          sync.RWMutex
	pending     map[string]*PendingCorrelation
	maxInFlight int
}

// PendingCorrelation represents an in-flight ask waiting for reply.
type PendingCorrelation struct {
	// CorrelationID is the ULID linking ask to reply
	CorrelationID string

	// ConsoleID is the console session this belongs to
	ConsoleID string

	// ActorID is the target actor namespace
	ActorID string

	// StartedAt is when the ask was sent
	StartedAt time.Time

	// Content is the original ask content (for display/retry)
	Content string

	// StreamedContent accumulates streaming event chunks
	StreamedContent string

	// Cancelled indicates if a cancel command was sent
	Cancelled bool
}

// NewCorrelationTracker creates a new correlation tracker.
//
// maxInFlight controls backpressure - typically 1 for sequential console I/O.
// Set to 0 to use the default of 1.
func NewCorrelationTracker(maxInFlight int) *CorrelationTracker {
	if maxInFlight <= 0 {
		maxInFlight = 1
	}
	return &CorrelationTracker{
		pending:     make(map[string]*PendingCorrelation),
		maxInFlight: maxInFlight,
	}
}

// NewCorrelation creates a new correlation for an ask.
//
// Returns the correlation ID on success, or ErrMaxInFlightReached if
// the maximum number of in-flight correlations has been reached.
func (ct *CorrelationTracker) NewCorrelation(consoleID, actorID, content string) (string, error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if len(ct.pending) >= ct.maxInFlight {
		return "", ErrMaxInFlightReached
	}

	correlID := ulid.Make().String()
	ct.pending[correlID] = &PendingCorrelation{
		CorrelationID: correlID,
		ConsoleID:     consoleID,
		ActorID:       actorID,
		StartedAt:     time.Now(),
		Content:       content,
	}
	return correlID, nil
}

// Complete marks a correlation as complete and removes it.
func (ct *CorrelationTracker) Complete(correlID string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	delete(ct.pending, correlID)
}

// Cancel marks a correlation as cancelled.
func (ct *CorrelationTracker) Cancel(correlID string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if p, ok := ct.pending[correlID]; ok {
		p.Cancelled = true
	}
}

// AppendStreamed appends streaming content to a correlation.
func (ct *CorrelationTracker) AppendStreamed(correlID, content string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if p, ok := ct.pending[correlID]; ok {
		p.StreamedContent += content
	}
}

// GetPending retrieves a pending correlation by ID.
// Returns nil if not found.
func (ct *CorrelationTracker) GetPending(correlID string) *PendingCorrelation {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	if p, ok := ct.pending[correlID]; ok {
		// Return a copy to avoid data races
		cp := *p
		return &cp
	}
	return nil
}

// GetActive returns the active (oldest) pending correlation.
// Returns nil if no correlations are pending.
func (ct *CorrelationTracker) GetActive() *PendingCorrelation {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	var oldest *PendingCorrelation
	for _, p := range ct.pending {
		if oldest == nil || p.StartedAt.Before(oldest.StartedAt) {
			cp := *p
			oldest = &cp
		}
	}
	return oldest
}

// ListPending returns all pending correlations.
func (ct *CorrelationTracker) ListPending() []PendingCorrelation {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	result := make([]PendingCorrelation, 0, len(ct.pending))
	for _, p := range ct.pending {
		result = append(result, *p)
	}
	return result
}

// Count returns the number of pending correlations.
func (ct *CorrelationTracker) Count() int {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return len(ct.pending)
}

// IsFull returns true if the maximum number of in-flight correlations has been reached.
func (ct *CorrelationTracker) IsFull() bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return len(ct.pending) >= ct.maxInFlight
}

// MaxInFlight returns the maximum number of in-flight correlations.
func (ct *CorrelationTracker) MaxInFlight() int {
	return ct.maxInFlight
}

// Clear removes all pending correlations.
func (ct *CorrelationTracker) Clear() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.pending = make(map[string]*PendingCorrelation)
}
