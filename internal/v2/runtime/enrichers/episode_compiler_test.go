package enrichers_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/v2/adapters/libsql/turns"
	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
	"github.com/joshka0/foxctl/internal/v2/runtime/enrichers"
	runtimeevents "github.com/joshka0/foxctl/internal/v2/runtime/events"
)

func TestEpisodeCompilerComponent_TurnRecordedCompilesAndSavesEpisodes(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 16,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	reader := &episodeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-ep-1": {ID: "turn-ep-1", SessionID: "run-ep-1", TurnIndex: 3},
		},
	}
	writer := &episodeWriterCapture{}
	compiler := episodeCompilerFunc(func(_ context.Context, turn run.TurnRecord) ([]run.EpisodeRecord, error) {
		return []run.EpisodeRecord{
			{
				ID:             "episode-1",
				EpisodeVersion: "v1",
				BoundaryKey:    "boundary:turn-ep-1",
				Topic:          "compiler skeleton",
			},
		}, nil
	})

	component := enrichers.NewEpisodeCompilerComponent(enrichers.EpisodeCompilerConfig{
		Bus:           bus,
		TurnReader:    reader,
		EpisodeWriter: writer,
		Compiler:      compiler,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()

	waitForEpisodeCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	if err := bus.Publish(context.Background(), coreevents.Event{
		ID:         "evt-ep-1",
		StreamID:   "run-ep-1",
		StreamType: coreevents.StreamTypeRun,
		EventType:  coreevents.EventTurnRecorded,
		Payload: coreevents.MustMarshalPayload(coreevents.TurnRecordedPayload{
			TurnID: "turn-ep-1",
		}),
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	waitForEpisodeCondition(t, 2*time.Second, func() bool {
		return writer.Len() == 1
	})

	got := writer.Get(0)
	if got.SessionID != "run-ep-1" {
		t.Fatalf("session_id=%q want run-ep-1", got.SessionID)
	}
	if got.StartTurnID != "turn-ep-1" || got.EndTurnID != "turn-ep-1" {
		t.Fatalf("turn span unexpected: start=%q end=%q", got.StartTurnID, got.EndTurnID)
	}
	if got.StartTurnIndex != 3 || got.EndTurnIndex != 3 {
		t.Fatalf("turn index span unexpected: start=%d end=%d", got.StartTurnIndex, got.EndTurnIndex)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component shutdown")
	}
}

func TestEpisodeCompilerComponent_CompilerErrorReportsWithoutBlocking(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 16,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	reader := &episodeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-ep-2": {ID: "turn-ep-2", SessionID: "run-ep-2"},
		},
	}
	writer := &episodeWriterCapture{}
	errs := make(chan error, 2)

	component := enrichers.NewEpisodeCompilerComponent(enrichers.EpisodeCompilerConfig{
		Bus:           bus,
		TurnReader:    reader,
		EpisodeWriter: writer,
		Compiler: episodeCompilerFunc(func(context.Context, run.TurnRecord) ([]run.EpisodeRecord, error) {
			return nil, errors.New("forced compiler failure")
		}),
		OnError: func(err error) {
			if err == nil {
				return
			}
			select {
			case errs <- err:
			default:
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()

	waitForEpisodeCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	start := time.Now()
	if err := bus.Publish(context.Background(), coreevents.Event{
		ID:         "evt-ep-2",
		StreamID:   "run-ep-2",
		StreamType: coreevents.StreamTypeRun,
		EventType:  coreevents.EventTurnRecorded,
		Payload: coreevents.MustMarshalPayload(coreevents.TurnRecordedPayload{
			TurnID: "turn-ep-2",
		}),
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Fatalf("publish blocked too long: %s", elapsed)
	}

	waitForEpisodeCondition(t, 2*time.Second, func() bool {
		stats := bus.Stats()
		return len(errs) > 0 || stats.Overflow > 0 || stats.Dropped > 0
	})
	if writer.Len() != 0 {
		t.Fatalf("writer len=%d want 0", writer.Len())
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component shutdown")
	}
}

func TestEpisodeCompilerComponent_SaveEpisodeErrorReportsWithoutBlocking(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 16,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	reader := &episodeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-ep-save-err": {ID: "turn-ep-save-err", SessionID: "run-ep-save-err", TurnIndex: 4},
		},
	}
	errs := make(chan error, 2)

	component := enrichers.NewEpisodeCompilerComponent(enrichers.EpisodeCompilerConfig{
		Bus:        bus,
		TurnReader: reader,
		EpisodeWriter: episodeWriterFunc(func(context.Context, run.EpisodeRecord) error {
			return errors.New("forced save failure")
		}),
		Compiler: episodeCompilerFunc(func(_ context.Context, _ run.TurnRecord) ([]run.EpisodeRecord, error) {
			return []run.EpisodeRecord{{
				ID:             "episode-save-error",
				EpisodeVersion: "v1",
				BoundaryKey:    "boundary:save-error",
			}}, nil
		}),
		OnError: func(err error) {
			if err == nil {
				return
			}
			select {
			case errs <- err:
			default:
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()

	waitForEpisodeCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	start := time.Now()
	if err := bus.Publish(context.Background(), coreevents.Event{
		ID:         "evt-ep-save-err",
		StreamID:   "run-ep-save-err",
		StreamType: coreevents.StreamTypeRun,
		EventType:  coreevents.EventTurnRecorded,
		Payload: coreevents.MustMarshalPayload(coreevents.TurnRecordedPayload{
			TurnID: "turn-ep-save-err",
		}),
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Fatalf("publish blocked too long: %s", elapsed)
	}

	waitForEpisodeCondition(t, 2*time.Second, func() bool {
		stats := bus.Stats()
		return len(errs) > 0 || stats.Overflow > 0 || stats.Dropped > 0
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component shutdown")
	}
}

func TestEpisodeCompilerComponent_InternalQueueFullDropsWithoutBlockingPublish(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 16,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	reader := &episodeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-ep-3": {ID: "turn-ep-3", SessionID: "run-ep-3", TurnIndex: 1},
		},
	}
	writer := &blockingEpisodeWriter{release: make(chan struct{})}

	component := enrichers.NewEpisodeCompilerComponent(enrichers.EpisodeCompilerConfig{
		Bus:           bus,
		TurnReader:    reader,
		EpisodeWriter: writer,
		Compiler: episodeCompilerFunc(func(_ context.Context, _ run.TurnRecord) ([]run.EpisodeRecord, error) {
			return []run.EpisodeRecord{{
				ID:             "episode-queue-test",
				EpisodeVersion: "v1",
				BoundaryKey:    "boundary:queue-test",
			}}, nil
		}),
		Buffer: 1,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()

	waitForEpisodeCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	start := time.Now()
	for i := 0; i < 20; i++ {
		if err := bus.Publish(context.Background(), coreevents.Event{
			ID:         fmt.Sprintf("evt-ep-queue-%02d", i),
			StreamID:   "run-ep-3",
			StreamType: coreevents.StreamTypeRun,
			EventType:  coreevents.EventTurnRecorded,
			Payload: coreevents.MustMarshalPayload(coreevents.TurnRecordedPayload{
				TurnID: "turn-ep-3",
			}),
		}); err != nil {
			t.Fatalf("Publish() error = %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("publish blocked too long: %s", elapsed)
	}

	close(writer.release)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component shutdown")
	}
}

func TestEpisodeCompilerComponent_DefaultCompilerWiring_UsesTimelineAndArtifacts(t *testing.T) {
	t.Parallel()

	bus := runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 16,
		OverflowPolicy:   runtimeevents.OverflowDropNewest,
	})
	createdAt := time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC)
	reader := &episodeTurnReader{
		turns: map[string]run.TurnRecord{
			"turn-ep-live-1": {
				ID:        "turn-ep-live-1",
				SessionID: "run-ep-live",
				TurnIndex: 1,
				Prompt:    "start task",
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
			},
			"turn-ep-live-2": {
				ID:          "turn-ep-live-2",
				SessionID:   "run-ep-live",
				TurnIndex:   2,
				Prompt:      "we decided to keep runtime wiring non-blocking",
				CreatedAt:   createdAt.Add(1 * time.Minute),
				UpdatedAt:   createdAt.Add(1 * time.Minute),
				FinalOutput: run.MessageRef{Text: "decision recorded"},
			},
		},
	}
	artifactReader := episodeArtifactReader{
		byTurn: map[string][]turns.Artifact{
			"turn-ep-live-1": {
				{TurnID: "turn-ep-live-1", ArtifactType: turns.ArtifactTypeAnnotation, ArtifactVersion: "v1", Ref: "artifact/turn-ep-live-1/annotation/v1"},
			},
			"turn-ep-live-2": {
				{TurnID: "turn-ep-live-2", ArtifactType: turns.ArtifactTypeClassification, ArtifactVersion: "v1", Ref: "artifact/turn-ep-live-2/classification/v1"},
			},
		},
	}
	writer := &episodeWriterCapture{}

	component := enrichers.NewEpisodeCompilerComponent(enrichers.EpisodeCompilerConfig{
		Bus:            bus,
		TurnReader:     reader,
		TurnTimeline:   reader,
		ArtifactReader: artifactReader,
		EpisodeWriter:  writer,
		BuildOptions: sourceimport.EpisodeBuildOptions{
			EpisodeVersion:     "v1",
			MaxTurnsPerEpisode: 2,
			Now:                func() time.Time { return createdAt },
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- component.Run(ctx) }()

	waitForEpisodeCondition(t, 2*time.Second, func() bool {
		return bus.Stats().Subscribers > 0
	})

	if err := bus.Publish(context.Background(), coreevents.Event{
		ID:         "evt-ep-live",
		StreamID:   "run-ep-live",
		StreamType: coreevents.StreamTypeRun,
		EventType:  coreevents.EventTurnRecorded,
		Payload: coreevents.MustMarshalPayload(coreevents.TurnRecordedPayload{
			TurnID: "turn-ep-live-2",
		}),
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	waitForEpisodeCondition(t, 2*time.Second, func() bool {
		return writer.Len() == 1
	})

	got := writer.Get(0)
	if got.BoundaryKey != "chunk:0001-0002:turn-ep-live-1-turn-ep-live-2" {
		t.Fatalf("boundary_key=%q want chunk:0001-0002:turn-ep-live-1-turn-ep-live-2", got.BoundaryKey)
	}
	if got.StartTurnID != "turn-ep-live-1" || got.EndTurnID != "turn-ep-live-2" {
		t.Fatalf("turn span=%q..%q want turn-ep-live-1..turn-ep-live-2", got.StartTurnID, got.EndTurnID)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component shutdown")
	}
}

type episodeTurnReader struct {
	mu    sync.Mutex
	turns map[string]run.TurnRecord
}

func (r *episodeTurnReader) GetTurn(_ context.Context, turnID string) (run.TurnRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	turn, ok := r.turns[turnID]
	if !ok {
		return run.TurnRecord{}, run.ErrTurnNotFound
	}
	return turn.Clone(), nil
}

func (r *episodeTurnReader) ListTurns(_ context.Context, sessionID string, _ run.TurnListOptions) ([]run.TurnRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]run.TurnRecord, 0, len(r.turns))
	for _, turn := range r.turns {
		if turn.SessionID != sessionID {
			continue
		}
		out = append(out, turn.Clone())
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TurnIndex != out[j].TurnIndex {
			return out[i].TurnIndex < out[j].TurnIndex
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

type episodeArtifactReader struct {
	byTurn map[string][]turns.Artifact
}

func (r episodeArtifactReader) ListArtifacts(_ context.Context, turnID string) ([]turns.Artifact, error) {
	items := r.byTurn[turnID]
	out := make([]turns.Artifact, 0, len(items))
	for _, item := range items {
		out = append(out, item.Clone())
	}
	return out, nil
}

type episodeWriterCapture struct {
	mu       sync.Mutex
	episodes []run.EpisodeRecord
}

type blockingEpisodeWriter struct {
	release chan struct{}
}

func (w *blockingEpisodeWriter) SaveEpisode(context.Context, run.EpisodeRecord) error {
	<-w.release
	return nil
}

func (w *episodeWriterCapture) SaveEpisode(_ context.Context, episode run.EpisodeRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.episodes = append(w.episodes, episode.Clone())
	return nil
}

func (w *episodeWriterCapture) Len() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.episodes)
}

func (w *episodeWriterCapture) Get(i int) run.EpisodeRecord {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.episodes[i].Clone()
}

type episodeCompilerFunc func(ctx context.Context, turn run.TurnRecord) ([]run.EpisodeRecord, error)

func (f episodeCompilerFunc) Compile(ctx context.Context, turn run.TurnRecord) ([]run.EpisodeRecord, error) {
	return f(ctx, turn)
}

type episodeWriterFunc func(ctx context.Context, episode run.EpisodeRecord) error

func (f episodeWriterFunc) SaveEpisode(ctx context.Context, episode run.EpisodeRecord) error {
	return f(ctx, episode)
}

func waitForEpisodeCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for condition")
}
