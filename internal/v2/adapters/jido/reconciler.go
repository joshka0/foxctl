package jido

import (
	"context"
	"fmt"
	"strings"
	"time"

	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
)

const (
	commandAsk        = "ask"
	askStreamPrefix   = "ask"
	defaultEventIDPre = "evt"
)

// EventAppender persists canonical v2 events.
type EventAppender interface {
	Append(ctx context.Context, event v2events.Event) error
}

// ProjectionApplier materializes v2 run/agent projections.
type ProjectionApplier interface {
	Apply(ctx context.Context, evt v2events.Event) error
}

// ReconcilerConfig configures event/projection reconciliation.
type ReconcilerConfig struct {
	Events      EventAppender
	Projections ProjectionApplier
	Now         func() time.Time
	NewID       func() string
}

// Reconciler appends runtime outcomes as canonical v2 events.
type Reconciler struct {
	events      EventAppender
	projections ProjectionApplier
	now         func() time.Time
	newID       func() string
}

// SignalCallback is the normalized callback payload from runtime to control plane.
type SignalCallback struct {
	AskID         string
	RequestID     string
	AgentID       string
	MessageID     string
	Status        string
	CorrelationID string
	CausationID   string
	Summary       string
	Error         string
	Metadata      map[string]any
}

// NewReconciler builds a runtime result reconciler.
func NewReconciler(cfg ReconcilerConfig) (*Reconciler, error) {
	if cfg.Events == nil {
		return nil, fmt.Errorf("event appender is required")
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.NewID == nil {
		seq := 0
		cfg.NewID = func() string {
			seq++
			return fmt.Sprintf("%s-%06d", defaultEventIDPre, seq)
		}
	}
	return &Reconciler{
		events:      cfg.Events,
		projections: cfg.Projections,
		now:         cfg.Now,
		newID:       cfg.NewID,
	}, nil
}

// RecordAskDispatched records an ask dispatch as a run.started event.
func (r *Reconciler) RecordAskDispatched(ctx context.Context, messageID string, signal SignalRequest) (v2events.Event, error) {
	if r == nil {
		return v2events.Event{}, fmt.Errorf("reconciler is not configured")
	}
	askID := strings.TrimSpace(signal.Signal.CorrelationID)
	if askID == "" {
		askID = strings.TrimSpace(signal.Signal.ID)
	}
	if askID == "" {
		return v2events.Event{}, fmt.Errorf("ask_id is required for dispatch event")
	}
	runID := askStreamID(askID)
	now := r.now()

	payload := v2events.MustMarshalPayload(map[string]any{
		"ask_id":      askID,
		"message_id":  strings.TrimSpace(messageID),
		"agent_id":    strings.TrimSpace(signal.AgentID),
		"status":      "sent",
		"signal_type": strings.TrimSpace(signal.Signal.Type),
	})

	evt := v2events.Event{
		ID:            prefixedID(defaultEventIDPre, r.newID),
		StreamID:      runID,
		StreamType:    v2events.StreamTypeRun,
		EventType:     v2events.EventRunStarted,
		OccurredAt:    now,
		CorrelationID: askID,
		CausationID:   strings.TrimSpace(signal.RequestID),
		ActorID:       strings.TrimSpace(signal.AgentID),
		RequestID:     strings.TrimSpace(signal.RequestID),
		Command:       commandAsk,
		Payload:       payload,
	}

	if err := r.appendAndProject(ctx, evt); err != nil {
		return v2events.Event{}, err
	}
	return evt, nil
}

// ReconcileSignalCallback records callback status as run.completed or run.failed.
func (r *Reconciler) ReconcileSignalCallback(ctx context.Context, cb SignalCallback) (v2events.Event, error) {
	if r == nil {
		return v2events.Event{}, fmt.Errorf("reconciler is not configured")
	}

	cb.AskID = strings.TrimSpace(cb.AskID)
	if cb.AskID == "" {
		return v2events.Event{}, fmt.Errorf("callback ask_id is required")
	}
	cb.Status = strings.ToLower(strings.TrimSpace(cb.Status))
	if cb.Status == "" {
		cb.Status = "completed"
	}

	eventType := v2events.EventRunCompleted
	if cb.Status == "failed" || cb.Status == "error" || strings.TrimSpace(cb.Error) != "" {
		eventType = v2events.EventRunFailed
	}

	now := r.now()
	payload := v2events.MustMarshalPayload(map[string]any{
		"ask_id":      cb.AskID,
		"message_id":  strings.TrimSpace(cb.MessageID),
		"status":      cb.Status,
		"summary":     strings.TrimSpace(cb.Summary),
		"error":       strings.TrimSpace(cb.Error),
		"metadata":    cb.Metadata,
		"agent_id":    strings.TrimSpace(cb.AgentID),
		"request_id":  strings.TrimSpace(cb.RequestID),
		"callback_at": now.UTC().Format(time.RFC3339Nano),
	})

	evt := v2events.Event{
		ID:            prefixedID(defaultEventIDPre, r.newID),
		StreamID:      askStreamID(cb.AskID),
		StreamType:    v2events.StreamTypeRun,
		EventType:     eventType,
		OccurredAt:    now,
		CorrelationID: chooseNonEmpty(cb.CorrelationID, cb.AskID),
		CausationID:   strings.TrimSpace(cb.CausationID),
		ActorID:       strings.TrimSpace(cb.AgentID),
		RequestID:     strings.TrimSpace(cb.RequestID),
		Command:       commandAsk,
		Payload:       payload,
	}

	if err := r.appendAndProject(ctx, evt); err != nil {
		return v2events.Event{}, err
	}
	return evt, nil
}

func (r *Reconciler) appendAndProject(ctx context.Context, evt v2events.Event) error {
	if err := r.events.Append(ctx, evt); err != nil {
		return err
	}
	if r.projections != nil {
		if err := r.projections.Apply(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}

func askStreamID(askID string) string {
	return askStreamPrefix + ":" + strings.TrimSpace(askID)
}

func chooseNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func prefixedID(prefix string, nextID func() string) string {
	if nextID == nil {
		return strings.TrimSpace(prefix)
	}
	return strings.TrimSpace(prefix) + "-" + strings.TrimSpace(nextID())
}
