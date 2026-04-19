package session

import (
	"context"
	"errors"
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

func TestOutputLog_SubscribeReplaysThenDelivers(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 10, MaxBytes: 1024})
	_, _ = l.Append([]byte("one"))
	_, _ = l.Append([]byte("two"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _, unsub := l.Subscribe(ctx, 0)
	defer unsub()

	// Replay should deliver both chunks.
	collected := collect(t, ch, 2, 500*time.Millisecond)
	if string(collected[0].Bytes) != "one" || string(collected[1].Bytes) != "two" {
		t.Fatalf("replay mismatch: %q %q", collected[0].Bytes, collected[1].Bytes)
	}

	_, _ = l.Append([]byte("three"))
	more := collect(t, ch, 1, 500*time.Millisecond)
	if string(more[0].Bytes) != "three" {
		t.Fatalf("live delivery mismatch: %q", more[0].Bytes)
	}
}

func TestOutputLog_SubscribeReplayFromFilters(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{MaxChunks: 10, MaxBytes: 1024})
	_, _ = l.Append([]byte("one"))
	_, _ = l.Append([]byte("two"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _, unsub := l.Subscribe(ctx, 1)
	defer unsub()

	got := collect(t, ch, 1, 500*time.Millisecond)
	if string(got[0].Bytes) != "two" {
		t.Fatalf("replayFrom=1 should skip seq 1, got %q", got[0].Bytes)
	}
}

func TestOutputLog_SubscribeCancelsWithContext(t *testing.T) {
	l := NewOutputLog(OutputLogOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	ch, _, _ := l.Subscribe(ctx, 0)
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
	ch, _, _ := l.Subscribe(ctx, 0)
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
	_, sub, unsub := l.Subscribe(ctx, 0)
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
