package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestOutputLog_AppendAssignsMonotonicSeq(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 10, MaxBytes: 1024})
	s1, err := l.Append([]byte("a"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	s2, err := l.Append([]byte("b"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if s1 != 1 || s2 != 2 {
		t.Errorf("seqs = %d,%d, want 1,2", s1, s2)
	}
	if l.NextSeq() != 3 {
		t.Errorf("NextSeq = %d, want 3", l.NextSeq())
	}
}

func TestOutputLog_AppendEmptyIsNoop(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{})
	seq, err := l.Append(nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if seq != 0 {
		t.Errorf("seq = %d, want 0", seq)
	}
	if l.Len() != 0 {
		t.Errorf("Len = %d, want 0", l.Len())
	}
}

func TestOutputLog_Since(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 10, MaxBytes: 1024})
	for i := 0; i < 5; i++ {
		if _, err := l.Append([]byte{byte('a' + i)}); err != nil {
			t.Fatal(err)
		}
	}
	got := l.Since(2, 0)
	if len(got) != 3 {
		t.Fatalf("Since(2) len = %d, want 3", len(got))
	}
	if got[0].Seq != 3 {
		t.Errorf("first seq = %d, want 3", got[0].Seq)
	}
}

func TestOutputLog_SinceRespectsLimit(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 10, MaxBytes: 1024})
	for i := 0; i < 5; i++ {
		_, _ = l.Append([]byte{byte('a' + i)})
	}
	got := l.Since(0, 2)
	if len(got) != 2 {
		t.Fatalf("Since(0,2) len = %d, want 2", len(got))
	}
	if got[0].Seq != 1 || got[1].Seq != 2 {
		t.Errorf("seqs = %d,%d", got[0].Seq, got[1].Seq)
	}
}

func TestOutputLog_EvictsByCount(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 3, MaxBytes: 1024})
	for i := 0; i < 5; i++ {
		_, _ = l.Append([]byte{byte('a' + i)})
	}
	if l.Len() != 3 {
		t.Fatalf("Len = %d, want 3", l.Len())
	}
	got := l.Since(0, 0)
	if got[0].Seq != 3 {
		t.Errorf("oldest retained seq = %d, want 3", got[0].Seq)
	}
	if got[len(got)-1].Seq != 5 {
		t.Errorf("newest seq = %d, want 5", got[len(got)-1].Seq)
	}
}

func TestOutputLog_EvictsByBytes(t *testing.T) {
	// budget 6 bytes => at most 3 two-byte chunks retained
	l := NewOutputLog(OutputLogOptions{MaxChunks: 100, MaxBytes: 6})
	_, _ = l.Append([]byte("aa"))
	_, _ = l.Append([]byte("bb"))
	_, _ = l.Append([]byte("cc"))
	_, _ = l.Append([]byte("dd"))
	if l.Len() != 3 {
		t.Fatalf("Len = %d, want 3", l.Len())
	}
	if l.Bytes() != 6 {
		t.Errorf("Bytes = %d, want 6", l.Bytes())
	}
}

func TestOutputLog_AppendAfterCloseIsError(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{})
	l.Close()
	_, err := l.Append([]byte("x"))
	if !errors.Is(err, ErrLogClosed) {
		t.Fatalf("want ErrLogClosed, got %v", err)
	}
}

func TestOutputLog_SubscribeFromReplaysThenDelivers(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 10, MaxBytes: 1024})
	_, _ = l.Append([]byte("one"))
	_, _ = l.Append([]byte("two"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	replay, ch, _, unsub := l.SubscribeFrom(ctx, 0)
	defer unsub()

	if len(replay) != 2 || string(replay[0].Bytes) != "one" || string(replay[1].Bytes) != "two" {
		t.Fatalf("replay mismatch: %+v", replay)
	}

	_, _ = l.Append([]byte("three"))
	more := collect(t, ch, 1, 500*time.Millisecond)
	if string(more[0].Bytes) != "three" {
		t.Fatalf("live delivery mismatch: %q", more[0].Bytes)
	}
}

func TestOutputLog_SubscribeFromFilters(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 10, MaxBytes: 1024})
	_, _ = l.Append([]byte("one"))
	_, _ = l.Append([]byte("two"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	replay, _, _, unsub := l.SubscribeFrom(ctx, 1)
	defer unsub()

	if len(replay) != 1 || string(replay[0].Bytes) != "two" {
		t.Fatalf("replayFrom=1 should skip seq 1, got %+v", replay)
	}
}

func TestOutputLog_SubscribeCancelsWithContext(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	ch, _, _ := l.Subscribe(ctx)
	cancel()
	// channel must eventually close.
	select {
	case _, ok := <-ch:
		if ok {
			// Drain any remaining, wait for close.
			for {
				if _, open := <-ch; !open {
					return
				}
			}
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("subscribe channel did not close on context cancel")
	}
}

func TestOutputLog_CloseReleasesSubscribers(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{})
	ctx := context.Background()
	ch, _, _ := l.Subscribe(ctx)
	l.Close()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel closed after log close")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("close did not release subscriber")
	}
}

func TestOutputLog_SubscriberDropsWhenBackpressured(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 10000, MaxBytes: 1 << 20})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, sub, unsub := l.Subscribe(ctx)
	defer unsub()

	// Never read from ch; all writes past capacity (256) must drop.
	for i := 0; i < 500; i++ {
		_, _ = l.Append([]byte{byte(i)})
	}
	// Give the notifier a tick to update dropped counts.
	time.Sleep(20 * time.Millisecond)
	if sub.Dropped() == 0 {
		t.Error("expected Dropped > 0 when subscriber is backpressured")
	}
}

// TestOutputLog_SubscriberCloseRaceDoesNotPanic exercises the previous
// "send on closed channel" panic: a client cancels its subscription while
// the Append path is notifying subscribers. This test would panic before the
// per-subscriber mutex fix.
func TestOutputLog_SubscriberCloseRaceDoesNotPanic(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 10000, MaxBytes: 1 << 20})
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _, _, cancel := l.SubscribeFrom(ctx, 0)
				// Immediate cancel races with any pending notify.
				cancel()
			}
		}()
	}

	wg.Add(1)
	stop := make(chan struct{})
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = l.Append([]byte("x"))
			}
		}
	}()

	// Let the race run briefly.
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestOutputLog_ReplayAndLiveAreInOrder ensures no live chunk slips ahead of
// a replay chunk even under concurrent append. Before the SubscribeFrom
// redesign, Subscribe's replay-after-unlock path could emit seq(replay_n)
// after seq(live_1).
func TestOutputLog_ReplayAndLiveAreInOrder(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 10000, MaxBytes: 1 << 20})
	// Seed retained history.
	for i := 0; i < 50; i++ {
		_, _ = l.Append([]byte{byte(i)})
	}

	// Append concurrently with subscribing to try to force interleaving.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = l.Append([]byte{0xFF})
			}
		}
	}()
	defer close(stop)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	replay, ch, _, unsub := l.SubscribeFrom(ctx, 0)
	defer unsub()

	// Last replay seq must be strictly less than first live seq.
	var lastReplaySeq uint64
	for _, c := range replay {
		if c.Seq <= lastReplaySeq {
			t.Fatalf("replay seq out of order: %d after %d", c.Seq, lastReplaySeq)
		}
		lastReplaySeq = c.Seq
	}
	// Drain a few live chunks and verify they're strictly after lastReplaySeq.
	deadline := time.After(500 * time.Millisecond)
	for seen := 0; seen < 20; {
		select {
		case c, ok := <-ch:
			if !ok {
				return
			}
			if c.Seq <= lastReplaySeq {
				t.Fatalf("live seq %d <= last replay seq %d", c.Seq, lastReplaySeq)
			}
			seen++
		case <-deadline:
			return
		}
	}
}

// TestOutputLog_SubscriberReceivesImmutableCopies verifies that mutating a
// chunk's bytes after delivery does not corrupt the log's retained state.
func TestOutputLog_SubscriberReceivesImmutableCopies(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 10, MaxBytes: 1024})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, ch, _, unsub := l.SubscribeFrom(ctx, ^uint64(0)) // live-only
	defer unsub()

	_, _ = l.Append([]byte("hello"))
	c := <-ch
	c.Bytes[0] = 'Z'

	retained := l.Since(0, 0)
	if len(retained) != 1 || string(retained[0].Bytes) != "hello" {
		t.Fatalf("subscriber mutation leaked into log: %q", retained[0].Bytes)
	}
}

func collect(t *testing.T, ch <-chan Chunk, n int, timeout time.Duration) []Chunk {
	t.Helper()
	out := make([]Chunk, 0, n)
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case c, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed after %d chunks, want %d", len(out), n)
			}
			out = append(out, c)
		case <-deadline:
			t.Fatalf("timeout waiting for %d chunks (got %d)", n, len(out))
		}
	}
	return out
}
