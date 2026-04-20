package runtime

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// simpleReq and simpleUpd are test-only types used by most tests.
type simpleReq struct {
	Val int
}
type simpleUpd struct {
	Val int
	Err error
}

// simpleHandler processes a request and returns an update.
func simpleHandler(_ context.Context, req simpleReq) simpleUpd {
	return simpleUpd{Val: req.Val * 2}
}

// errorHandler always returns an error update.
func errorHandler(_ context.Context, _ simpleReq) simpleUpd {
	return simpleUpd{Err: errors.New("handler failed")}
}

// blockingHandler blocks until its context is cancelled.
func blockingHandler(ctx context.Context, _ simpleReq) simpleUpd {
	<-ctx.Done()
	return simpleUpd{Err: ctx.Err()}
}

// drainUpdates reads all updates from the channel until it closes or times out.
func drainUpdates(t *testing.T, ch <-chan simpleUpd) []simpleUpd {
	t.Helper()
	var result []simpleUpd
	for {
		select {
		case upd, ok := <-ch:
			if !ok {
				return result
			}
			result = append(result, upd)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out draining updates")
			return result
		}
	}
}

// readUpdate reads one update or times out.
func readUpdate(t *testing.T, ch <-chan simpleUpd) simpleUpd {
	t.Helper()
	select {
	case upd, ok := <-ch:
		if !ok {
			t.Fatal("updates channel closed unexpectedly")
		}
		return upd
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for update")
		return simpleUpd{}
	}
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// TestBounded_ConfigurableBufferSize verifies (i): bounded buffer size is
// configurable at construction and items beyond the buffer block.
func TestBounded_ConfigurableBufferSize(t *testing.T) {
	t.Parallel()

	// Create a handler that blocks so requests stay in-flight and fill the buffer.
	// With buffer=1: handler picks up req1 (blocks), buffer has 1 slot, req2 fills it, req3 blocks.
	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		1, // request buffer = 1
		4, // update buffer (plenty of room)
		blockingHandler,
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}
	defer b.Stop()

	// First enqueue goes to handler goroutine (blocks on ctx).
	if err := b.Enqueue(context.Background(), simpleReq{Val: 1}); err != nil {
		t.Fatalf("Enqueue 1 error: %v", err)
	}
	// Second enqueue fills the one buffer slot.
	if err := b.Enqueue(context.Background(), simpleReq{Val: 2}); err != nil {
		t.Fatalf("Enqueue 2 error: %v", err)
	}

	// Third enqueue should block because handler is busy AND buffer is full.
	enqueueDone := make(chan error, 1)
	go func() {
		enqueueDone <- b.Enqueue(context.Background(), simpleReq{Val: 3})
	}()

	select {
	case <-enqueueDone:
		t.Fatal("third enqueue should have blocked on full buffer")
	case <-time.After(100 * time.Millisecond):
		// Expected: still blocking.
	}

	// Stop unblocks everything.
	b.Stop()

	select {
	case err := <-enqueueDone:
		if err == nil {
			t.Fatal("blocked enqueue should return error after Stop")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("blocked enqueue did not unblock after Stop")
	}
}

// TestBounded_StopIsIdempotent verifies (ii): Stop() is safe to call
// concurrently twice without panic and completes within 100ms.
func TestBounded_StopIsIdempotent(t *testing.T) {
	t.Parallel()

	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		4,
		4,
		simpleHandler,
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		b.Stop()
	}()
	go func() {
		defer wg.Done()
		b.Stop()
	}()
	wg.Wait()
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Fatalf("concurrent double Stop took %v, want <100ms", elapsed)
	}
}

// TestBounded_EnqueueOnStoppedReturnsErrStopped verifies (iii): Enqueue on a
// stopped runtime returns ErrStopped and does not send.
func TestBounded_EnqueueOnStoppedReturnsErrStopped(t *testing.T) {
	t.Parallel()

	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		4,
		4,
		simpleHandler,
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}

	b.Stop()

	err = b.Enqueue(context.Background(), simpleReq{Val: 1})
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("Enqueue after Stop error = %v, want ErrStopped", err)
	}
}

// TestBounded_UpdatesClosedOnce verifies (iv): Updates() is closed exactly
// once after drain.
func TestBounded_UpdatesClosedOnce(t *testing.T) {
	t.Parallel()

	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		4,
		4,
		simpleHandler,
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}

	if err := b.Enqueue(context.Background(), simpleReq{Val: 1}); err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}
	if err := b.Enqueue(context.Background(), simpleReq{Val: 2}); err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}

	b.Stop()

	// Read remaining items, then confirm channel closes.
	_ = drainUpdates(t, b.Updates())

	// We should get at least one update before the channel closes.
	// (The handler may or may not have processed both before Stop.)
	// len(upds) == 0 is fine if Stop was called before handler processed
	// anything. The important thing is the channel is closed (verified below).

	// Verify channel is closed by reading again.
	_, ok := <-b.Updates()
	if ok {
		t.Fatal("Updates() channel should be closed after Stop()")
	}

	// Reading from Updates() again still returns closed.
	_, ok = <-b.Updates()
	if ok {
		t.Fatal("Updates() channel should remain closed on second read")
	}
}

// TestBounded_ContextCancelDuringEnqueue verifies (v): ctx cancellation during
// a blocked Enqueue returns ctx.Err().
func TestBounded_ContextCancelDuringEnqueue(t *testing.T) {
	t.Parallel()

	// buffer=1: handler takes req1 (blocks), buffer fills with req2, req3 blocks.
	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		1,
		4,
		blockingHandler,
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}
	defer b.Stop()

	// First enqueue goes to handler (blocks).
	if err := b.Enqueue(context.Background(), simpleReq{Val: 1}); err != nil {
		t.Fatalf("Enqueue 1 error: %v", err)
	}
	// Second enqueue fills the buffer.
	if err := b.Enqueue(context.Background(), simpleReq{Val: 2}); err != nil {
		t.Fatalf("Enqueue 2 error: %v", err)
	}

	// Third enqueue should block.
	ctx, cancel := context.WithCancel(context.Background())
	enqueueDone := make(chan error, 1)
	go func() {
		enqueueDone <- b.Enqueue(ctx, simpleReq{Val: 3})
	}()

	select {
	case <-enqueueDone:
		t.Fatal("enqueue should be blocking")
	case <-time.After(100 * time.Millisecond):
		// Expected: still blocking.
	}

	cancel()

	select {
	case err := <-enqueueDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("blocked enqueue error = %v, want context.Canceled", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("blocked enqueue did not unblock after context cancel")
	}
}

// TestBounded_NoGoroutineLeak verifies (vi): no goroutine leak after Stop.
func TestBounded_NoGoroutineLeak(t *testing.T) {
	t.Parallel()

	// Force GC to get a clean baseline.
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	before := runtime.NumGoroutine()

	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		4,
		4,
		simpleHandler,
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}

	// Enqueue a few items.
	if err := b.Enqueue(context.Background(), simpleReq{Val: 1}); err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}
	if err := b.Enqueue(context.Background(), simpleReq{Val: 2}); err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}

	b.Stop()

	// Give goroutines time to exit.
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()

	delta := after - before
	if delta > 0 {
		t.Fatalf("goroutine leak: before=%d after=%d delta=%d", before, after, delta)
	}
}

// TestBounded_ConcurrentEnqueueWithStop verifies (vii): 8 concurrent Enqueue
// + 1 concurrent Stop — no panic, no deadlock. Deterministic over -count=50.
func TestBounded_ConcurrentEnqueueWithStop(t *testing.T) {
	t.Parallel()

	var enqueued atomic.Int64

	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		8,
		8,
		simpleHandler,
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}

	var wg sync.WaitGroup

	// Launch 8 concurrent enqueuers.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			err := b.Enqueue(context.Background(), simpleReq{Val: val})
			if err == nil {
				enqueued.Add(1)
			}
		}(i)
	}

	// Concurrently stop after a short delay.
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		b.Stop()
	}()

	// Wait for all goroutines to finish — this would deadlock if there's a bug.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success: no panic, no deadlock.
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock detected: concurrent enqueue + stop did not complete within 5s")
	}

	// Drain updates to confirm the channel eventually closes.
	_ = drainUpdates(t, b.Updates())
}

// TestBounded_EnqueueBlocksWhenFullUntilDrained verifies bounded backpressure:
// Enqueue blocks when the request buffer is full and unblocks when a slot is freed.
func TestBounded_EnqueueBlocksWhenFullUntilDrained(t *testing.T) {
	t.Parallel()

	// Use a handler that blocks until we signal it, giving us full timing control.
	unblock := make(chan struct{})
	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		1, // request buffer = 1
		4,
		func(ctx context.Context, req simpleReq) simpleUpd {
			<-unblock
			return simpleUpd{Val: req.Val * 2}
		},
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}
	defer b.Stop()

	// First enqueue: handler picks it up and blocks on <-unblock.
	if err := b.Enqueue(context.Background(), simpleReq{Val: 1}); err != nil {
		t.Fatalf("Enqueue 1 error: %v", err)
	}

	// Brief sleep to let handler pick up req1.
	time.Sleep(20 * time.Millisecond)

	// Second enqueue fills the one buffer slot (handler busy with req1).
	if err := b.Enqueue(context.Background(), simpleReq{Val: 2}); err != nil {
		t.Fatalf("Enqueue 2 error: %v", err)
	}

	// Third enqueue should block (buffer full, handler busy).
	enqueueDone := make(chan error, 1)
	go func() {
		enqueueDone <- b.Enqueue(context.Background(), simpleReq{Val: 3})
	}()

	select {
	case <-enqueueDone:
		t.Fatal("third enqueue should block on full buffer")
	case <-time.After(100 * time.Millisecond):
		// Expected.
	}

	// Unblock the handler so it processes req1, then picks up req2 freeing a slot.
	close(unblock)

	// Now the third enqueue should succeed.
	select {
	case err := <-enqueueDone:
		if err != nil {
			t.Fatalf("third enqueue error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("third enqueue did not unblock after drain")
	}

	// Stop the runtime to close the updates channel.
	b.Stop()

	// Drain remaining updates.
	_ = drainUpdates(t, b.Updates())
}

// TestBounded_CloseAfterStopIsNoop verifies Close() idempotency: calling Close
// after Stop is safe and does nothing.
func TestBounded_CloseAfterStopIsNoop(t *testing.T) {
	t.Parallel()

	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		4,
		4,
		simpleHandler,
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}

	b.Stop()
	// Close should be safe to call after Stop.
	b.Close()
	b.Close() // Double close is also fine.
}

// TestBounded_ProcessesAllEnqueuedBeforeStop verifies that items enqueued before
// Stop are processed.
func TestBounded_ProcessesAllEnqueuedBeforeStop(t *testing.T) {
	t.Parallel()

	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		8,
		8,
		simpleHandler,
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}

	for i := 1; i <= 4; i++ {
		if err := b.Enqueue(context.Background(), simpleReq{Val: i}); err != nil {
			t.Fatalf("Enqueue %d error: %v", i, err)
		}
	}

	// Give the handler time to process.
	time.Sleep(100 * time.Millisecond)
	b.Stop()

	upds := drainUpdates(t, b.Updates())
	if len(upds) != 4 {
		t.Fatalf("len(updates) = %d, want 4", len(upds))
	}
	// Verify all values are processed: Val * 2.
	expected := map[int]bool{2: true, 4: true, 6: true, 8: true}
	for _, upd := range upds {
		if !expected[upd.Val] {
			t.Fatalf("unexpected update.Val = %d", upd.Val)
		}
		delete(expected, upd.Val)
	}
	if len(expected) != 0 {
		t.Fatalf("missing updates: %v", expected)
	}
}

// TestBounded_HandlerErrorPropagates verifies that handler errors are delivered
// as updates.
func TestBounded_HandlerErrorPropagates(t *testing.T) {
	t.Parallel()

	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		4,
		4,
		errorHandler,
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}
	defer b.Stop()

	if err := b.Enqueue(context.Background(), simpleReq{Val: 1}); err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}

	upd := readUpdate(t, b.Updates())
	if upd.Err == nil {
		t.Fatal("expected error in update, got nil")
	}
	if upd.Err.Error() != "handler failed" {
		t.Fatalf("update.Err = %q, want %q", upd.Err.Error(), "handler failed")
	}
}

// TestBounded_NilReceiverStopIsNoop verifies Stop on nil is safe.
func TestBounded_NilReceiverStopIsNoop(t *testing.T) {
	t.Parallel()

	var b *Bounded[simpleReq, simpleUpd]
	b.Stop()  // Should not panic.
	b.Close() // Should not panic.
}

// TestBounded_NilHandlerPanics verifies that a nil handler is rejected at
// construction time.
func TestBounded_NilHandlerRejected(t *testing.T) {
	t.Parallel()

	_, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		4,
		4,
		nil,
	)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

// TestBounded_NegativeBufferSizeRejected verifies that negative buffer sizes
// are rejected at construction time.
func TestBounded_NegativeBufferSizeRejected(t *testing.T) {
	t.Parallel()

	_, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		-1,
		4,
		simpleHandler,
	)
	if err == nil {
		t.Fatal("expected error for negative request buffer size")
	}

	_, err = NewBounded[simpleReq, simpleUpd](
		context.Background(),
		4,
		-1,
		simpleHandler,
	)
	if err == nil {
		t.Fatal("expected error for negative update buffer size")
	}
}

// TestBounded_DefaultBufferSize verifies that buffer size of 0 defaults to 16.
func TestBounded_DefaultBufferSize(t *testing.T) {
	t.Parallel()

	b, err := NewBounded[simpleReq, simpleUpd](
		context.Background(),
		0, // should default to 16
		0, // should default to 16
		simpleHandler,
	)
	if err != nil {
		t.Fatalf("NewBounded error: %v", err)
	}
	defer b.Stop()

	// Should be able to enqueue 16 items without blocking.
	for i := 0; i < 16; i++ {
		if err := b.Enqueue(context.Background(), simpleReq{Val: i}); err != nil {
			t.Fatalf("Enqueue %d error: %v", i, err)
		}
	}
}
