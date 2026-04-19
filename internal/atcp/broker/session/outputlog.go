package session

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Chunk is one append to a session's output log.
//
// Seq is monotonic per log; bytes are the raw PTY output captured in order.
// Chunks are immutable once appended; readers receive defensive copies of
// Bytes so they cannot mutate the log state.
type Chunk struct {
	Seq       uint64
	Bytes     []byte
	Timestamp time.Time
}

// OutputLog is a bounded, append-only in-memory record of PTY output.
//
// The log enforces two budgets: a maximum chunk count and a maximum total
// byte footprint. When either is exceeded, the oldest chunks are dropped from
// the head. Seq is monotonic across the lifetime of the log and is never
// reused, so replay calls can rely on gaps indicating evicted data.
//
// The log is safe for concurrent Append and Since calls. Append is single-writer
// in practice (the owning session goroutine); Since and Subscribe may be
// called from any goroutine.
type OutputLog struct {
	mu       sync.Mutex
	chunks   []Chunk
	maxCount int
	maxBytes int
	curBytes int
	nextSeq  uint64
	subs     map[*subscriber]struct{}
	closed   bool
}

// OutputLogOptions configures a log's budgets. Zero values fall back to defaults
// tuned for short-running interactive agents (8 KiB chunks, ~4 MiB total).
type OutputLogOptions struct {
	MaxChunks int
	MaxBytes  int
}

// DefaultOutputLogOptions returns the default budget. The values are sized so
// a 2000-line scrollback at ~80 cols fits comfortably in memory while the
// broker remains responsive on constrained hosts.
func DefaultOutputLogOptions() OutputLogOptions {
	return OutputLogOptions{
		MaxChunks: 4096,
		MaxBytes:  4 * 1024 * 1024,
	}
}

// NewOutputLog constructs an empty log using the supplied options (or defaults
// when opts is the zero value).
func NewOutputLog(opts OutputLogOptions) *OutputLog {
	if opts.MaxChunks <= 0 {
		opts.MaxChunks = DefaultOutputLogOptions().MaxChunks
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultOutputLogOptions().MaxBytes
	}
	return &OutputLog{
		chunks:   make([]Chunk, 0, opts.MaxChunks/4+1),
		maxCount: opts.MaxChunks,
		maxBytes: opts.MaxBytes,
		subs:     make(map[*subscriber]struct{}),
		nextSeq:  1,
	}
}

// ErrLogClosed is returned by Append after Close.
var ErrLogClosed = errors.New("atcp session: output log is closed")

// Append records a copy of data as the next chunk and returns its Seq.
//
// A defensive copy is made so callers may reuse the input buffer (e.g. a PTY
// read buffer) immediately. Evicts oldest chunks when budgets are exceeded.
// Appending an empty slice returns 0 and does not emit a subscription notify.
func (l *OutputLog) Append(data []byte) (uint64, error) {
	if len(data) == 0 {
		return 0, nil
	}
	buf := make([]byte, len(data))
	copy(buf, data)

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return 0, ErrLogClosed
	}
	seq := l.nextSeq
	l.nextSeq++
	chunk := Chunk{Seq: seq, Bytes: buf, Timestamp: time.Now().UTC()}
	l.chunks = append(l.chunks, chunk)
	l.curBytes += len(buf)
	l.evictLocked()

	subs := make([]*subscriber, 0, len(l.subs))
	for s := range l.subs {
		subs = append(subs, s)
	}
	l.mu.Unlock()

	for _, s := range subs {
		s.notify(chunk)
	}
	return seq, nil
}

// Since returns a copy of all chunks with Seq > since, up to limit entries. A
// limit of 0 means no limit. Chunks include a copy of their Bytes so callers
// may hold them past the log's eviction horizon.
func (l *OutputLog) Since(since uint64, limit int) []Chunk {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Chunk, 0)
	for _, c := range l.chunks {
		if c.Seq <= since {
			continue
		}
		out = append(out, cloneChunk(c))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Len returns the number of chunks currently retained.
func (l *OutputLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.chunks)
}

// NextSeq returns the seq that the next Append call will produce.
func (l *OutputLog) NextSeq() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.nextSeq
}

// Bytes returns the total bytes currently retained across all chunks.
func (l *OutputLog) Bytes() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.curBytes
}

// Close marks the log closed. Further Append calls return ErrLogClosed, and all
// active subscribers are released.
func (l *OutputLog) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	subs := make([]*subscriber, 0, len(l.subs))
	for s := range l.subs {
		subs = append(subs, s)
	}
	l.subs = nil
	l.mu.Unlock()
	for _, s := range subs {
		s.close()
	}
}

// Subscribe returns a live channel for new chunks appended after the call.
//
// The returned cancel function unsubscribes and closes the channel. The
// subscription is bounded: if the reader falls behind, chunks are dropped and
// the subscription's Dropped counter advances.
//
// Replay is *not* part of Subscribe. Callers that need retained history plus a
// gap-free live stream must use SubscribeFrom instead — Subscribe atomically
// registered the subscriber only for future appends, so concurrent Append
// calls between a prior Since() and Subscribe() would otherwise be lost.
func (l *OutputLog) Subscribe(ctx context.Context) (<-chan Chunk, *Subscription, func()) {
	replay, ch, s, cancel := l.SubscribeFrom(ctx, ^uint64(0))
	_ = replay // Subscribe discards history by construction
	return ch, s, cancel
}

// SubscribeFrom atomically captures retained chunks with Seq > from and a
// live subscription, under the same lock, so callers are guaranteed that the
// first chunk arriving on the channel has Seq strictly greater than the last
// replay entry.
//
// The returned replay slice contains defensive copies of bytes and is owned
// by the caller. Callers should drain it synchronously before reading the
// channel to preserve ordering. The channel is bounded; slow readers will
// experience drops tracked on the Subscription.
func (l *OutputLog) SubscribeFrom(ctx context.Context, from uint64) ([]Chunk, <-chan Chunk, *Subscription, func()) {
	sub := &subscriber{
		ch:   make(chan Chunk, 256),
		done: make(chan struct{}),
	}
	s := &Subscription{sub: sub}

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		close(sub.ch)
		close(sub.done)
		return nil, sub.ch, s, func() {}
	}
	var replay []Chunk
	if from != ^uint64(0) {
		for _, c := range l.chunks {
			if c.Seq > from {
				replay = append(replay, cloneChunk(c))
			}
		}
	}
	// Registering the subscriber here — before releasing the lock — is what
	// makes this API gap-free: no Append can interleave between the replay
	// snapshot and the first live notify.
	l.subs[sub] = struct{}{}
	l.mu.Unlock()

	cancel := func() {
		l.mu.Lock()
		delete(l.subs, sub)
		l.mu.Unlock()
		sub.close()
	}

	// Tie subscription lifetime to ctx.
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-sub.done:
		}
	}()

	return replay, sub.ch, s, cancel
}

// Subscription exposes drop statistics for an active Subscribe call.
type Subscription struct {
	sub *subscriber
}

// Dropped returns the number of chunks that were dropped because the receiver
// fell behind. Useful for emitting a viewer.dropped_frames metric.
func (s *Subscription) Dropped() uint64 {
	if s == nil || s.sub == nil {
		return 0
	}
	return s.sub.droppedLoad()
}

// evictLocked trims the log when it exceeds either budget. Caller must hold l.mu.
func (l *OutputLog) evictLocked() {
	for len(l.chunks) > l.maxCount || l.curBytes > l.maxBytes {
		if len(l.chunks) == 0 {
			return
		}
		drop := l.chunks[0]
		l.chunks = l.chunks[1:]
		l.curBytes -= len(drop.Bytes)
	}
}

func cloneChunk(c Chunk) Chunk {
	buf := make([]byte, len(c.Bytes))
	copy(buf, c.Bytes)
	return Chunk{Seq: c.Seq, Bytes: buf, Timestamp: c.Timestamp}
}
