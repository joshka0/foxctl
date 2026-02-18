package enrichers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/observability"
	coreevents "github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

var (
	// ErrMissingSubscriber indicates event subscriber dependency is nil.
	ErrMissingSubscriber = errors.New("v2 enrichers: missing event subscriber")
	// ErrMissingTurnReader indicates turn reader dependency is nil.
	ErrMissingTurnReader = errors.New("v2 enrichers: missing turn reader")
	// ErrMissingTurnID indicates turn.recorded payload did not include turn_id.
	ErrMissingTurnID = errors.New("v2 enrichers: missing turn id")
)

const (
	defaultProducerBuffer = 64
)

// ArtifactSpec configures one artifact derivation target.
type ArtifactSpec struct {
	Type    string
	Version string
}

// EventSubscriber subscribes to runtime event fanout.
type EventSubscriber interface {
	Subscribe(buffer int) (<-chan coreevents.Event, func())
}

// ProducerConfig wires the turn-recorded -> queue producer.
type ProducerConfig struct {
	Bus           EventSubscriber
	Queue         *Queue
	TurnReader    run.TurnReader
	Buffer        int
	ArtifactSpecs []ArtifactSpec
	OnError       func(error)
}

// Producer consumes runtime events and enqueues enrichment jobs.
type Producer struct {
	bus           EventSubscriber
	queue         *Queue
	turnReader    run.TurnReader
	buffer        int
	artifactSpecs []ArtifactSpec
	onError       func(error)
}

// NewProducer creates a non-blocking enricher producer.
func NewProducer(cfg ProducerConfig) *Producer {
	if cfg.Buffer <= 0 {
		cfg.Buffer = defaultProducerBuffer
	}
	if cfg.OnError == nil {
		cfg.OnError = func(err error) {
			if err == nil {
				return
			}
			observability.Emit(context.Background(), observability.NewEvent("v2.runtime.enricher.producer.error").
				WithComponent(observability.ComponentAgent).
				Error(err, 0))
		}
	}
	specs := normalizeArtifactSpecs(cfg.ArtifactSpecs)
	if len(specs) == 0 {
		specs = []ArtifactSpec{
			{Type: "embedding", Version: "v1"},
			{Type: "annotation", Version: "v1"},
			{Type: "classification", Version: "v1"},
			{Type: "learning", Version: "v1"},
		}
	}

	return &Producer{
		bus:           cfg.Bus,
		queue:         cfg.Queue,
		turnReader:    cfg.TurnReader,
		buffer:        cfg.Buffer,
		artifactSpecs: specs,
		onError:       cfg.OnError,
	}
}

// Run subscribes to runtime events and enqueues artifact jobs for turn.recorded.
func (p *Producer) Run(ctx context.Context) error {
	if p == nil || p.bus == nil {
		return ErrMissingSubscriber
	}
	if p.queue == nil {
		return ErrMissingQueue
	}
	if p.turnReader == nil {
		return ErrMissingTurnReader
	}

	eventsCh, unsubscribe := p.bus.Subscribe(p.buffer)
	defer unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-eventsCh:
			if !ok {
				return nil
			}
			p.handleEvent(ctx, evt)
		}
	}
}

func (p *Producer) handleEvent(ctx context.Context, evt coreevents.Event) {
	if evt.EventType != coreevents.EventTurnRecorded {
		return
	}

	turnID, err := turnIDFromEvent(evt)
	if err != nil {
		p.onError(err)
		return
	}

	turn, err := p.turnReader.GetTurn(ctx, turnID)
	if err != nil {
		p.onError(err)
		return
	}

	for _, spec := range p.artifactSpecs {
		job := NewJob(turn, spec.Type, spec.Version)
		if accepted := p.queue.Enqueue(job); !accepted {
			p.onError(fmt.Errorf(
				"v2 enrichers: dropped enqueue turn_id=%s artifact_type=%s artifact_version=%s",
				job.TurnID, job.ArtifactType, job.ArtifactVersion,
			))
		}
	}
}

func turnIDFromEvent(evt coreevents.Event) (string, error) {
	var payload coreevents.TurnRecordedPayload
	if len(evt.Payload) > 0 {
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return "", fmt.Errorf("decode turn.recorded payload: %w", err)
		}
	}
	turnID := strings.TrimSpace(payload.TurnID)
	if turnID == "" && evt.StreamType == coreevents.StreamTypeTurn {
		turnID = strings.TrimSpace(evt.StreamID)
	}
	if turnID == "" {
		return "", ErrMissingTurnID
	}
	return turnID, nil
}

func normalizeArtifactSpecs(specs []ArtifactSpec) []ArtifactSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]ArtifactSpec, 0, len(specs))
	for _, spec := range specs {
		t := strings.TrimSpace(strings.ToLower(spec.Type))
		v := strings.TrimSpace(spec.Version)
		if t == "" {
			continue
		}
		if v == "" {
			v = "v1"
		}
		out = append(out, ArtifactSpec{Type: t, Version: v})
	}
	return out
}
