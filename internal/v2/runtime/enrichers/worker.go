package enrichers

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/observability"
	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

var (
	// ErrMissingQueue indicates the worker queue dependency is nil.
	ErrMissingQueue = errors.New("v2 enrichers: missing queue")
	// ErrMissingEnricher indicates the enricher dependency is nil.
	ErrMissingEnricher = errors.New("v2 enrichers: missing enricher")
)

// Enricher performs one artifact derivation over a persisted turn.
type Enricher interface {
	Enrich(ctx context.Context, job Job) error
}

// EnricherFunc adapts functions to the Enricher interface.
type EnricherFunc func(ctx context.Context, job Job) error

// Enrich runs the function as an Enricher.
func (f EnricherFunc) Enrich(ctx context.Context, job Job) error {
	return f(ctx, job)
}

// Config wires enrichment worker behavior.
type Config struct {
	Queue      *Queue
	Enricher   Enricher
	EventStore coreevents.Appender
	Now        func() time.Time
	NewID      func() string
	OnError    func(error)
}

// Worker consumes enrichment jobs asynchronously and emits failure events.
type Worker struct {
	queue      *Queue
	enricher   Enricher
	eventStore coreevents.Appender
	now        func() time.Time
	newID      func() string
	onError    func(error)

	seq atomic.Int64
}

// NewWorker creates an enrichment worker.
func NewWorker(cfg Config) *Worker {
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.NewID == nil {
		var local atomic.Uint64
		cfg.NewID = func() string {
			return fmt.Sprintf("artifact-failed-%d", local.Add(1))
		}
	}
	if cfg.OnError == nil {
		cfg.OnError = func(err error) {
			if err == nil {
				return
			}
			observability.Emit(context.Background(), observability.NewEvent("v2.runtime.enricher.error").
				WithComponent(observability.ComponentAgent).
				Error(err, 0))
		}
	}

	return &Worker{
		queue:      cfg.Queue,
		enricher:   cfg.Enricher,
		eventStore: cfg.EventStore,
		now:        cfg.Now,
		newID:      cfg.NewID,
		onError:    cfg.OnError,
	}
}

// Run blocks until context cancellation or queue close.
func (w *Worker) Run(ctx context.Context) error {
	if w == nil || w.queue == nil {
		return ErrMissingQueue
	}
	if w.enricher == nil {
		return ErrMissingEnricher
	}

	jobs := w.queue.Jobs()
	for {
		select {
		case <-ctx.Done():
			return nil
		case job, ok := <-jobs:
			if !ok {
				return nil
			}
			w.process(ctx, job)
		}
	}
}

func (w *Worker) process(ctx context.Context, job Job) {
	if w.queue != nil {
		defer w.queue.Release(job)
	}

	err := w.enricher.Enrich(ctx, job)
	if err == nil {
		return
	}

	w.onError(err)
	if w.eventStore == nil {
		return
	}

	payload := coreevents.ErrorPayload{
		Kind:      "artifact_failed",
		Message:   "artifact enrichment failed",
		Cause:     err.Error(),
		Retryable: true,
		Details: map[string]any{
			"turn_id":          job.Turn.ID,
			"artifact_type":    job.ArtifactType,
			"artifact_version": job.ArtifactVersion,
		},
	}
	raw, marshalErr := coreevents.MarshalPayload(payload)
	if marshalErr != nil {
		w.onError(marshalErr)
		return
	}

	seq := w.seq.Add(1)
	evt := coreevents.Event{
		ID:            w.newID(),
		StreamID:      job.Turn.ID,
		StreamType:    coreevents.StreamTypeTurn,
		StreamVersion: seq,
		Sequence:      seq,
		EventType:     coreevents.EventArtifactFailed,
		OccurredAt:    w.now().UTC(),
		CorrelationID: job.Turn.CorrelationID,
		CausationID:   job.Turn.CausationID,
		ActorID:       job.Turn.ActorID,
		RequestID:     job.Turn.RequestID,
		Command:       job.Turn.Command,
		Payload:       raw,
	}
	if appendErr := w.eventStore.Append(ctx, evt); appendErr != nil {
		w.onError(appendErr)
	}
}

// NewJob builds a normalized enrichment job from a turn record.
func NewJob(turn run.TurnRecord, artifactType, artifactVersion string) Job {
	return Job{
		TurnID:          turn.ID,
		ArtifactType:    artifactType,
		ArtifactVersion: artifactVersion,
		Turn:            turn.Clone(),
	}
}
