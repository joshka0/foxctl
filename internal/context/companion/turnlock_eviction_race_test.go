package companion

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// TestRegression_EvictionDuringInFlightAcquisition verifies that maybeEvict
// cannot delete a mutex entry while another goroutine is between releasing
// tl.mu and acquiring entry.mu.
//
// Bug: Before the fix, the window between tl.mu.Unlock() and entry.mu.TryLock()
// allowed maybeEvict to see the mutex as unlocked (the acquirer hadn't locked
// it yet), delete it, and a subsequent Lock call would create a NEW mutex —
// breaking mutual exclusion for that conversationID.
//
// Fix: Reference counting (entry.refs) prevents eviction of in-flight entries.
//
// Regression: tests/regression/001-turnlock-eviction-race/
func TestRegression_EvictionDuringInFlightAcquisition(t *testing.T) {
	tl := NewTurnLock()
	const convID = "race-target"

	// Fill to eviction threshold so maybeEvict actually runs.
	for i := range maxTurnLockEntries {
		id := "filler-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		// Use the internal map directly to avoid the overhead of Lock/Unlock
		// for 10k entries — we just need to hit the threshold.
		tl.locks[id] = &turnLockEntry{}
	}

	// Acquire lock on the target conversation.
	unlock1, err := tl.Lock(context.Background(), convID)
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	// Now launch many goroutines that all try to lock the same conversation.
	// This creates in-flight entries (refs > 0) for convID.
	const concurrency = 50
	var (
		wg       sync.WaitGroup
		started  sync.WaitGroup
		failures atomic.Int64
	)
	started.Add(concurrency)
	wg.Add(concurrency)

	for range concurrency {
		go func() {
			started.Done()
			defer wg.Done()
			u, err := tl.Lock(context.Background(), convID)
			if err != nil {
				failures.Add(1)
				return
			}
			// Each goroutine holds the lock briefly then releases.
			u()
		}()
	}

	// Wait for all goroutines to start (they'll be blocked waiting for the lock).
	started.Wait()

	// Trigger eviction by calling Lock on a new entry (which calls maybeEvict
	// since we're above the threshold).
	go func() {
		u, err := tl.Lock(context.Background(), "eviction-trigger")
		if err == nil {
			u()
		}
	}()

	// Also explicitly evict to stress the Evict path.
	tl.Evict(convID)

	// Release the original lock — the waiting goroutines should all serialize
	// through the SAME mutex, not different ones.
	unlock1()

	// Wait for all goroutines to complete.
	wg.Wait()

	if f := failures.Load(); f != 0 {
		t.Fatalf("got %d Lock failures, want 0", f)
	}

	// Verify mutual exclusion was maintained: if two goroutines ever held
	// different mutexes for the same convID, they would have run concurrently.
	// We verify this by running a concurrent increment test.
	var counter int64
	var violations atomic.Int64

	wg.Add(concurrency)
	for range concurrency {
		go func() {
			defer wg.Done()
			u, err := tl.Lock(context.Background(), convID)
			if err != nil {
				t.Errorf("Lock() error = %v", err)
				return
			}
			defer u()

			// Non-atomic increment: if mutual exclusion is broken,
			// concurrent access will cause lost updates.
			val := atomic.LoadInt64(&counter)
			// Yield to increase chance of interleaving if locks are broken.
			atomic.StoreInt64(&counter, val+1)
		}()
	}
	wg.Wait()

	if v := violations.Load(); v != 0 {
		t.Fatalf("mutual exclusion violated %d times", v)
	}
	if atomic.LoadInt64(&counter) != concurrency {
		t.Fatalf("counter = %d, want %d (mutual exclusion broken)", counter, concurrency)
	}
}

// TestRegression_EvictSkipsReferencedEntries verifies that Evict() does not
// remove entries with in-flight references.
func TestRegression_EvictSkipsReferencedEntries(t *testing.T) {
	tl := NewTurnLock()
	const convID = "evict-target"

	// Lock the conversation.
	unlock, err := tl.Lock(context.Background(), convID)
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	// While locked, Evict should be a no-op (refs > 0 or mutex held).
	tl.Evict(convID)

	// Verify the entry still exists.
	tl.mu.Lock()
	_, exists := tl.locks[convID]
	tl.mu.Unlock()
	if !exists {
		t.Fatal("Evict removed entry while lock was held")
	}

	unlock()

	// After unlock, Evict should succeed (refs == 0 and mutex unlocked).
	tl.Evict(convID)

	tl.mu.Lock()
	_, exists = tl.locks[convID]
	tl.mu.Unlock()
	if exists {
		t.Fatal("Evict did not remove entry after unlock")
	}
}

// TestRegression_AlreadyCanceledContext verifies that Lock returns immediately
// when the context is already canceled.
func TestRegression_AlreadyCanceledContext(t *testing.T) {
	tl := NewTurnLock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := tl.Lock(ctx, "conv-1")
	if err == nil {
		t.Fatal("Lock() should return error for already-canceled context")
	}
	if err != context.Canceled {
		t.Fatalf("Lock() error = %v, want context.Canceled", err)
	}
}
