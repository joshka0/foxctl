package companion

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// turnLockEntry holds a per-conversation mutex and a reference count.
// refs tracks the number of goroutines that hold or are attempting to acquire
// the mutex. Eviction skips entries where refs > 0, preventing a race where
// an in-flight acquisition (between releasing tl.mu and acquiring entry.mu)
// could be invalidated by a concurrent eviction.
type turnLockEntry struct {
	mu   sync.Mutex
	refs int // holders + waiters + in-flight acquisitions
}

// TurnLock provides per-conversation mutual exclusion for turn processing.
//
// TurnLock maintains a per-conversation mutex map and performs bounded eviction
// of unlocked entries to avoid unbounded memory growth in long-running servers.
type TurnLock struct {
	mu    sync.Mutex
	locks map[string]*turnLockEntry
}

var _ Locker = (*TurnLock)(nil)

// NewTurnLock creates an initialized TurnLock.
func NewTurnLock() *TurnLock {
	return &TurnLock{locks: make(map[string]*turnLockEntry)}
}

// maxTurnLockEntries is the upper bound before eviction sweeps unused entries.
// This prevents unbounded growth of the locks map in long-running servers.
const maxTurnLockEntries = 10000

// Lock acquires the per-conversation mutex, respecting context cancellation.
// Returns an unlock function and nil error on success.
// Returns an error if the context is cancelled before the lock is acquired.
func (tl *TurnLock) Lock(ctx context.Context, conversationID string) (unlock func(), err error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, errors.New("conversation ID must not be empty")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// Check for already-canceled context before taking the global lock.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tl.mu.Lock()
	if tl.locks == nil {
		tl.locks = make(map[string]*turnLockEntry)
	}
	tl.maybeEvict()
	entry, ok := tl.locks[conversationID]
	if !ok {
		entry = &turnLockEntry{}
		tl.locks[conversationID] = entry
	}
	// Increment refs while holding tl.mu so that eviction cannot remove this
	// entry between releasing tl.mu and acquiring entry.mu.
	entry.refs++
	tl.mu.Unlock()

	// decRef decrements the reference count while holding tl.mu.
	decRef := func() {
		tl.mu.Lock()
		entry.refs--
		tl.mu.Unlock()
	}

	// Fast path: uncontended acquisition without spawning a goroutine.
	if entry.mu.TryLock() {
		return func() {
			entry.mu.Unlock()
			decRef()
		}, nil
	}

	// Slow path: make acquisition cancellable.
	locked := make(chan struct{})
	go func() {
		entry.mu.Lock()
		close(locked)
	}()

	select {
	case <-locked:
		return func() {
			entry.mu.Unlock()
			decRef()
		}, nil
	case <-ctx.Done():
		// We started a goroutine that will eventually acquire the lock.
		// We need to unlock it and release the ref when it does.
		go func() {
			<-locked
			entry.mu.Unlock()
			decRef()
		}()
		return nil, ctx.Err()
	}
}

// Evict removes the mutex for a conversation.
//
// If the mutex is currently held or has in-flight acquisition attempts,
// the eviction is skipped to preserve serialization for in-flight turns.
func (tl *TurnLock) Evict(conversationID string) {
	if strings.TrimSpace(conversationID) == "" {
		return
	}

	tl.mu.Lock()
	defer tl.mu.Unlock()
	if tl.locks == nil {
		return
	}
	if entry, ok := tl.locks[conversationID]; ok {
		if entry.refs > 0 {
			return // in-flight acquisitions or holders, skip eviction
		}
		if entry.mu.TryLock() {
			entry.mu.Unlock()
			delete(tl.locks, conversationID)
		}
		// else: mutex is held, skip eviction
	}
}

func (tl *TurnLock) maybeEvict() {
	if tl.locks == nil {
		return
	}
	if len(tl.locks) < maxTurnLockEntries {
		return
	}
	for id, entry := range tl.locks {
		if entry.refs > 0 {
			continue // in-flight acquisitions or holders, skip
		}
		if entry.mu.TryLock() {
			// Was unlocked (idle) -- safe to evict.
			entry.mu.Unlock()
			delete(tl.locks, id)
		}
	}
}
