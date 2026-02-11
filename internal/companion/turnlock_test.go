package companion

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

func recvOrTimeout[T any](t *testing.T, ch <-chan T, d time.Duration) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(d):
		t.Fatalf("timeout waiting on channel")
		var zero T
		return zero
	}
}

// TestTurnLock_SerializesSameConversation verifies turns for the same conversation are serialized.
func TestTurnLock_SerializesSameConversation(t *testing.T) {
	tl := NewTurnLock()

	start := make(chan struct{})
	entered := make(chan int, 2)
	exited := make(chan int, 2)

	release := map[int]chan struct{}{
		1: make(chan struct{}),
		2: make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(2)
	for id := 1; id <= 2; id++ {
		id := id
		go func() {
			defer wg.Done()
			<-start

			unlock, err := tl.Lock(context.Background(), "conv1")
			if err != nil {
				t.Errorf("Lock: %v", err)
				return
			}
			entered <- id
			<-release[id]
			exited <- id
			unlock()
		}()
	}

	close(start)

	first := recvOrTimeout(t, entered, 2*time.Second)
	select {
	case second := <-entered:
		t.Fatalf("expected serialization; second goroutine entered while first still held lock (second=%d, first=%d)", second, first)
	default:
	}

	close(release[first])
	ex := recvOrTimeout(t, exited, 2*time.Second)
	if ex != first {
		t.Fatalf("expected first goroutine to exit first, got %d (want %d)", ex, first)
	}

	second := recvOrTimeout(t, entered, 2*time.Second)
	if second == first {
		t.Fatalf("expected different goroutine to enter second, got %d", second)
	}

	close(release[second])
	ex2 := recvOrTimeout(t, exited, 2*time.Second)
	if ex2 != second {
		t.Fatalf("expected second goroutine to exit second, got %d (want %d)", ex2, second)
	}

	wg.Wait()
}

// TestTurnLock_AllowsDifferentConversationsConcurrently verifies different conversation IDs do not block each other.
func TestTurnLock_AllowsDifferentConversationsConcurrently(t *testing.T) {
	tl := NewTurnLock()

	start := make(chan struct{})
	entered := make(chan string, 2)
	release := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		unlock, err := tl.Lock(context.Background(), "a")
		if err != nil {
			t.Errorf("Lock(a): %v", err)
			return
		}
		entered <- "a"
		<-release
		unlock()
	}()

	go func() {
		defer wg.Done()
		<-start
		unlock, err := tl.Lock(context.Background(), "b")
		if err != nil {
			t.Errorf("Lock(b): %v", err)
			return
		}
		entered <- "b"
		<-release
		unlock()
	}()

	close(start)

	// If there were a global lock, we'd only see one entry before releasing.
	_ = recvOrTimeout(t, entered, 2*time.Second)
	_ = recvOrTimeout(t, entered, 2*time.Second)

	close(release)
	wg.Wait()
}

// TestTurnLock_EvictCreatesNewMutex verifies eviction removes the entry so the next lock creates a fresh mutex.
func TestTurnLock_EvictCreatesNewMutex(t *testing.T) {
	tl := NewTurnLock()

	unlock, err := tl.Lock(context.Background(), "conv")
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	unlock()

	tl.mu.Lock()
	m1 := tl.locks["conv"]
	tl.mu.Unlock()

	tl.Evict("conv")

	unlock2, err := tl.Lock(context.Background(), "conv")
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	unlock2()

	tl.mu.Lock()
	m2 := tl.locks["conv"]
	tl.mu.Unlock()

	if m1 == m2 {
		t.Fatalf("expected new mutex after eviction")
	}
}

// TestTurnLock_ConcurrentLockSameIDCreatesOneEntry verifies concurrent calls only create one per-conversation entry.
func TestTurnLock_ConcurrentLockSameIDCreatesOneEntry(t *testing.T) {
	tl := NewTurnLock()

	const n = 50
	start := make(chan struct{})
	errs := make(chan error, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			unlock, err := tl.Lock(context.Background(), "same")
			if err != nil {
				errs <- err
				return
			}
			unlock()
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("Lock: %v", err)
		}
	}

	tl.mu.Lock()
	defer tl.mu.Unlock()
	if got := len(tl.locks); got != 1 {
		t.Fatalf("expected exactly 1 lock entry, got %d", got)
	}
}

// TestTurnLock_BoundedEviction_EvictsIdleEntries verifies idle entries are evicted when the map grows too large.
func TestTurnLock_BoundedEviction_EvictsIdleEntries(t *testing.T) {
	tl := NewTurnLock()

	// Fill the map to the threshold with idle (unlocked) entries.
	for i := 0; i < maxTurnLockEntries; i++ {
		unlock, err := tl.Lock(context.Background(), fmt.Sprintf("idle-%d", i))
		if err != nil {
			t.Fatalf("Lock: %v", err)
		}
		unlock()
	}

	// Map should be at the limit.
	tl.mu.Lock()
	before := len(tl.locks)
	tl.mu.Unlock()
	if before != maxTurnLockEntries {
		t.Fatalf("expected %d entries before eviction, got %d", maxTurnLockEntries, before)
	}

	// Acquiring a new ID should trigger eviction of idle entries.
	unlock, err := tl.Lock(context.Background(), "new-entry")
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	unlock()

	tl.mu.Lock()
	after := len(tl.locks)
	tl.mu.Unlock()

	// After eviction, idle entries should have been cleared and only the new
	// entry should remain (plus any that couldn't be evicted).
	if after >= maxTurnLockEntries {
		t.Fatalf("expected eviction to reduce map size below %d, got %d", maxTurnLockEntries, after)
	}
}

// TestTurnLock_BoundedEviction_PreservesLockedEntries verifies locked entries are not evicted by bounded eviction.
func TestTurnLock_BoundedEviction_PreservesLockedEntries(t *testing.T) {
	tl := NewTurnLock()

	// Fill the map to the threshold, but hold some entries locked.
	const lockedCount = 5
	lockedUnlocks := make([]func(), 0, lockedCount)
	for i := 0; i < lockedCount; i++ {
		unlock, err := tl.Lock(context.Background(), fmt.Sprintf("locked-%d", i))
		if err != nil {
			t.Fatalf("Lock: %v", err)
		}
		lockedUnlocks = append(lockedUnlocks, unlock)
	}

	// Fill the rest with idle entries.
	for i := 0; i < maxTurnLockEntries-lockedCount; i++ {
		unlock, err := tl.Lock(context.Background(), fmt.Sprintf("idle-%d", i))
		if err != nil {
			t.Fatalf("Lock: %v", err)
		}
		unlock()
	}

	// Trigger eviction.
	unlock, err := tl.Lock(context.Background(), "trigger")
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	unlock()

	tl.mu.Lock()
	after := len(tl.locks)
	tl.mu.Unlock()

	// Locked entries (lockedCount) plus the new entry should survive.
	// Idle entries should have been evicted.
	expectedMin := lockedCount + 1
	if after < expectedMin {
		t.Fatalf("expected at least %d entries (locked + new), got %d", expectedMin, after)
	}
	if after > lockedCount+2 {
		// Allow some slack, but it should be much less than the original.
		t.Fatalf("expected most idle entries evicted; got %d entries (locked=%d)", after, lockedCount)
	}

	// Unlock the held mutexes.
	for _, unlock := range lockedUnlocks {
		unlock()
	}
}

// TestTurnLock_Lock_ContextCancelledDoesNotLeakGoroutines verifies cancellation returns an error and internal goroutines drain.
func TestTurnLock_Lock_ContextCancelledDoesNotLeakGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()

	tl := NewTurnLock()

	for i := 0; i < 25; i++ {
		holdUnlock, err := tl.Lock(context.Background(), "conv")
		if err != nil {
			t.Fatalf("Lock(hold): %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			unlock, err := tl.Lock(ctx, "conv")
			if err == nil {
				unlock()
			}
			done <- err
		}()

		cancel()
		err = recvOrTimeout(t, done, 2*time.Second)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}

		// Allow the internal cleanup goroutine to acquire/unlock once we release.
		holdUnlock()

		ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
		unlock2, err := tl.Lock(ctx2, "conv")
		cancel2()
		if err != nil {
			t.Fatalf("Lock(after cancel): %v", err)
		}
		unlock2()
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+5 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	after := runtime.NumGoroutine()
	if after > before+5 {
		t.Fatalf("possible goroutine leak: before=%d after=%d", before, after)
	}
}

// TestTurnLock_Evict_SkipsLockedEntries verifies eviction is skipped for in-flight locks.
func TestTurnLock_Evict_SkipsLockedEntries(t *testing.T) {
	tl := NewTurnLock()

	unlock, err := tl.Lock(context.Background(), "conv")
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	tl.Evict("conv")

	tl.mu.Lock()
	_, ok := tl.locks["conv"]
	tl.mu.Unlock()
	if !ok {
		t.Fatalf("expected eviction to be skipped while lock is held")
	}

	unlock()

	tl.Evict("conv")
	tl.mu.Lock()
	_, ok = tl.locks["conv"]
	tl.mu.Unlock()
	if ok {
		t.Fatalf("expected eviction to remove unlocked entry")
	}
}
