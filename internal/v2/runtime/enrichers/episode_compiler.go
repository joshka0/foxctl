package enrichers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

var (
	// ErrMissingEpisodeWriter indicates episode writer dependency is nil.
	ErrMissingEpisodeWriter = errors.New("v2 enrichers: missing episode writer")
	// ErrMissingEpisodeCompiler indicates episode compiler dependency is nil.
	ErrMissingEpisodeCompiler = errors.New("v2 enrichers: missing episode compiler")
)

// EpisodeCompiler derives episode records for a turn in the async pipeline.
type EpisodeCompiler interface {
	Compile(ctx context.Context, turn run.TurnRecord) ([]run.EpisodeRecord, error)
}

// NoopEpisodeCompiler is a safe default that emits no episodes.
type NoopEpisodeCompiler struct{}

// Compile returns no episodes.
func (NoopEpisodeCompiler) Compile(context.Context, run.TurnRecord) ([]run.EpisodeRecord, error) {
	return nil, nil
}

// EpisodeCompilerConfig wires the turn-recorded -> episode compile component.
type EpisodeCompilerConfig struct {
	Bus            EventSubscriber
	TurnReader     run.TurnReader
	TurnTimeline   run.TurnTimelineReader
	ArtifactReader sourceimport.TurnArtifactReader
	EpisodeWriter  run.EpisodeWriter
	Compiler       EpisodeCompiler
	BuildOptions   sourceimport.EpisodeBuildOptions
	Buffer         int
	OnError        func(error)
}

// EpisodeCompilerComponent asynchronously compiles episodes from turn events.
type EpisodeCompilerComponent struct {
	bus           EventSubscriber
	turnReader    run.TurnReader
	episodeWriter run.EpisodeWriter
	compiler      EpisodeCompiler
	buffer        int
	onError       func(error)
}

// NewEpisodeCompilerComponent creates a non-blocking episode compiler component.
func NewEpisodeCompilerComponent(cfg EpisodeCompilerConfig) *EpisodeCompilerComponent {
	fallbackNoop := false
	if cfg.Buffer <= 0 {
		cfg.Buffer = defaultProducerBuffer
	}
	if cfg.Compiler == nil {
		if cfg.TurnTimeline != nil && cfg.ArtifactReader != nil {
			cfg.Compiler = sourceimport.NewSessionEpisodeCompiler(cfg.TurnTimeline, cfg.ArtifactReader, cfg.BuildOptions)
		} else {
			cfg.Compiler = NoopEpisodeCompiler{}
			fallbackNoop = true
		}
	}
	if cfg.OnError == nil {
		cfg.OnError = func(err error) {
			if err == nil {
				return
			}
			observability.Emit(context.Background(), observability.NewEvent("v2.runtime.enricher.episode.error").
				WithComponent(observability.ComponentAgent).
				Error(err, 0))
		}
	}
	if fallbackNoop {
		cfg.OnError(errors.New("v2 enrichers: episode compiler defaulted to noop (missing TurnTimeline or ArtifactReader)"))
	}
	return &EpisodeCompilerComponent{
		bus:           cfg.Bus,
		turnReader:    cfg.TurnReader,
		episodeWriter: cfg.EpisodeWriter,
		compiler:      cfg.Compiler,
		buffer:        cfg.Buffer,
		onError:       cfg.OnError,
	}
}

// Run subscribes to runtime events and compiles episodes for turn.recorded.
func (c *EpisodeCompilerComponent) Run(ctx context.Context) error {
	if c == nil || c.bus == nil {
		return ErrMissingSubscriber
	}
	if c.turnReader == nil {
		return ErrMissingTurnReader
	}
	if c.episodeWriter == nil {
		return ErrMissingEpisodeWriter
	}
	if c.compiler == nil {
		return ErrMissingEpisodeCompiler
	}

	eventsCh, unsubscribe := c.bus.Subscribe(c.buffer)
	defer unsubscribe()
	jobs := make(chan coreevents.Event, c.buffer)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-jobs:
				if !ok {
					return
				}
				c.handleEvent(ctx, evt)
			}
		}
	}()
	defer func() {
		close(jobs)
		wg.Wait()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-eventsCh:
			if !ok {
				return nil
			}
			select {
			case jobs <- evt:
			default:
				c.onError(fmt.Errorf("v2 enrichers: dropped episode compile event type=%s stream_id=%s", evt.EventType, evt.StreamID))
			}
		}
	}
}

func (c *EpisodeCompilerComponent) handleEvent(ctx context.Context, evt coreevents.Event) {
	if evt.EventType != coreevents.EventTurnRecorded {
		return
	}
	turnID, err := turnIDFromEvent(evt)
	if err != nil {
		c.onError(err)
		return
	}
	turn, err := c.turnReader.GetTurn(ctx, turnID)
	if err != nil {
		c.onError(err)
		return
	}
	episodes, err := c.compiler.Compile(ctx, turn)
	if err != nil {
		c.onError(err)
		return
	}

	for _, episode := range episodes {
		normalized := episode.Clone()
		missingStartID := normalized.StartTurnID == ""
		missingEndID := normalized.EndTurnID == ""
		if normalized.SessionID == "" {
			normalized.SessionID = turn.SessionID
		}
		if missingStartID {
			normalized.StartTurnID = turn.ID
		}
		if missingEndID {
			normalized.EndTurnID = turn.ID
		}
		if missingStartID {
			normalized.StartTurnIndex = turn.TurnIndex
		}
		if missingEndID {
			normalized.EndTurnIndex = turn.TurnIndex
		}
		if err := c.episodeWriter.SaveEpisode(ctx, normalized); err != nil {
			c.onError(err)
		}
	}
}
