package runner

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func TestPipeline_NextTurnIndex_SeedsFromTimelineOncePerRun(t *testing.T) {
	t.Parallel()

	rec := &seedTimelineRecorder{
		turns: []run.TurnRecord{
			{ID: "turn-seed-41", SessionID: "run-seeded", TurnIndex: 41},
		},
	}
	p := New(Config{TurnRecorder: rec})

	first := p.nextTurnIndex(context.Background(), "run-seeded")
	second := p.nextTurnIndex(context.Background(), "run-seeded")

	if first != 42 {
		t.Fatalf("first nextTurnIndex=%d want 42", first)
	}
	if second != 43 {
		t.Fatalf("second nextTurnIndex=%d want 43", second)
	}
	if calls := rec.calls.Load(); calls != 1 {
		t.Fatalf("timeline list calls=%d want 1", calls)
	}
}

func TestPipeline_NextTurnIndex_ConcurrentSeed_IsUniqueAndSingleLookup(t *testing.T) {
	t.Parallel()

	rec := &seedTimelineRecorder{
		turns: []run.TurnRecord{
			{ID: "turn-seed-41", SessionID: "run-seeded", TurnIndex: 41},
		},
	}
	p := New(Config{TurnRecorder: rec})

	const workers = 2
	results := make(chan int, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- p.nextTurnIndex(context.Background(), "run-seeded")
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	seen := map[int]int{}
	for got := range results {
		seen[got]++
	}

	if seen[42] != 1 || seen[43] != 1 || len(seen) != 2 {
		t.Fatalf("concurrent nextTurnIndex results=%v want {42:1,43:1}", seen)
	}
	if calls := rec.calls.Load(); calls != 1 {
		t.Fatalf("timeline list calls=%d want 1", calls)
	}
}

func TestPipeline_NextTurnIndex_EvictsOldRunsWhenCapacityExceeded(t *testing.T) {
	t.Parallel()

	p := New(Config{})
	for i := 0; i < turnSeqMaxEntries+32; i++ {
		runID := fmt.Sprintf("run-%04d", i)
		if got := p.nextTurnIndex(context.Background(), runID); got != 1 {
			t.Fatalf("nextTurnIndex(%q)=%d want 1", runID, got)
		}
	}

	p.turnSeqMu.Lock()
	defer p.turnSeqMu.Unlock()
	if got := len(p.turnSeq); got > turnSeqMaxEntries {
		t.Fatalf("turnSeq size=%d exceeds max=%d", got, turnSeqMaxEntries)
	}
	if got := len(p.turnSeen); got > turnSeqMaxEntries {
		t.Fatalf("turnSeen size=%d exceeds max=%d", got, turnSeqMaxEntries)
	}
}

type seedTimelineRecorder struct {
	turns []run.TurnRecord
	calls atomic.Int32
}

func (r *seedTimelineRecorder) SaveTurn(_ context.Context, _ run.TurnRecord) error { return nil }

func (r *seedTimelineRecorder) ListTurns(_ context.Context, sessionID string, opts run.TurnListOptions) ([]run.TurnRecord, error) {
	r.calls.Add(1)
	if opts.Limit <= 0 {
		opts.Limit = 1
	}
	out := make([]run.TurnRecord, 0, opts.Limit)
	for _, turn := range r.turns {
		if sessionID != "" && turn.SessionID != sessionID {
			continue
		}
		out = append(out, turn.Clone())
		if len(out) == opts.Limit {
			break
		}
	}
	return out, nil
}
