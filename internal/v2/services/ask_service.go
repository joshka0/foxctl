package services

import (
	"context"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/v2/core/ask"
	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	"github.com/joshka0/foxctl/internal/v2/core/events"
)

// AskDependencies wires AskService collaborators.
type AskDependencies struct {
	Policy      AskPolicyAuthorizer
	Dispatcher  AskDispatcher
	Events      events.Appender
	Projections AskProjectionApplier
	DefaultTTL  time.Duration
	Now         func() time.Time
	NewID       func() string
}

// AskProjectionApplier materializes ask-related events into read models.
type AskProjectionApplier interface {
	Apply(ctx context.Context, evt events.Event) error
}

// AskService centralizes ask validation, policy checks, and dispatch mapping.
type AskService struct {
	deps AskDependencies
}

// NewAskService builds an ask service with defaults.
func NewAskService(deps AskDependencies) *AskService {
	if deps.NewID == nil {
		deps.NewID = defaultNewID()
	}
	if deps.DefaultTTL <= 0 {
		deps.DefaultTTL = 5 * time.Minute
	}
	if deps.Now == nil {
		deps.Now = defaultNow()
	}
	return &AskService{deps: deps}
}

// Ask validates request, enforces policy, and dispatches a normalized ask message.
func (s *AskService) Ask(ctx context.Context, req ask.Request) (ask.Response, error) {
	if s == nil || s.deps.Dispatcher == nil {
		return ask.Response{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "ask dispatcher is not configured",
			Fatal:   true,
		}
	}

	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		return ask.Response{}, asValidationError("question is required", map[string]any{
			"field": "question",
		})
	}

	req.AgentID = strings.TrimSpace(req.AgentID)
	req.Namespace = strings.TrimSpace(req.Namespace)
	if req.AgentID == "" && req.Namespace == "" {
		return ask.Response{}, asValidationError("agent_id or namespace is required", map[string]any{
			"field": "agent_id",
		})
	}

	req.RequestID = normalizeOrGenerate(req.RequestID, defaultRequestPref, s.deps.NewID)
	req.AskID = normalizeOrGenerate(req.AskID, defaultAskPref, s.deps.NewID)
	req.Kind = strings.TrimSpace(req.Kind)
	if req.Kind == "" {
		req.Kind = defaultAskKind
	}

	if s.deps.Policy != nil {
		if err := s.deps.Policy.AuthorizeAsk(ctx, req); err != nil {
			return ask.Response{}, asPolicyError(err)
		}
	}

	toNS := req.Namespace
	if toNS == "" {
		// For v2 service-level parity we allow direct namespace fallback to agent ID.
		toNS = req.AgentID
	}
	msg := ask.Message{
		AskID:          req.AskID,
		RequestID:      req.RequestID,
		Kind:           req.Kind,
		Question:       req.Question,
		ConversationID: strings.TrimSpace(req.ConversationID),
		FromNS:         requestToCallerNS(req.CallerNS, s.deps.NewID),
		ToNS:           toNS,
		TTLMS:          parsePositiveDurationOrDefault(req.Timeout, s.deps.DefaultTTL).Milliseconds(),
	}

	if err := s.recordDispatch(ctx, req, msg, msg.AskID); err != nil {
		return ask.Response{}, err
	}
	messageID, err := s.deps.Dispatcher.Send(ctx, msg)
	if err != nil {
		_ = s.recordDispatchFailure(ctx, req, msg, err)
		return ask.Response{}, asDependencyError("dispatch ask message", err)
	}

	return ask.Response{
		AskID:     msg.AskID,
		MessageID: strings.TrimSpace(messageID),
		AgentID:   req.AgentID,
		Namespace: toNS,
		Status:    "sent",
	}, nil
}

func (s *AskService) recordDispatch(ctx context.Context, req ask.Request, msg ask.Message, messageID string) error {
	if s.deps.Events == nil {
		return nil
	}
	askID := strings.TrimSpace(msg.AskID)
	now := s.deps.Now()
	evt := events.Event{
		ID:            prefixedID("evt", s.deps.NewID),
		StreamID:      "ask:" + askID,
		StreamType:    events.StreamTypeRun,
		EventType:     events.EventRunStarted,
		OccurredAt:    now,
		CorrelationID: askID,
		CausationID:   strings.TrimSpace(msg.RequestID),
		ActorID:       strings.TrimSpace(req.AgentID),
		RequestID:     strings.TrimSpace(msg.RequestID),
		Command:       "ask",
		Payload: events.MustMarshalPayload(map[string]any{
			"ask_id":     askID,
			"message_id": strings.TrimSpace(messageID),
			"kind":       strings.TrimSpace(msg.Kind),
			"question":   strings.TrimSpace(msg.Question),
			"agent_id":   strings.TrimSpace(req.AgentID),
			"namespace":  strings.TrimSpace(msg.ToNS),
			"status":     "dispatching",
		}),
	}
	if err := s.deps.Events.Append(ctx, evt); err != nil {
		return asDependencyError("append ask event", err)
	}
	if s.deps.Projections != nil {
		if err := s.deps.Projections.Apply(ctx, evt); err != nil {
			return asDependencyError("apply ask projection", err)
		}
	}
	return nil
}

func (s *AskService) recordDispatchFailure(ctx context.Context, req ask.Request, msg ask.Message, dispatchErr error) error {
	if s.deps.Events == nil {
		return nil
	}
	askID := strings.TrimSpace(msg.AskID)
	now := s.deps.Now()
	evt := events.Event{
		ID:            prefixedID("evt", s.deps.NewID),
		StreamID:      "ask:" + askID,
		StreamType:    events.StreamTypeRun,
		EventType:     events.EventRunFailed,
		OccurredAt:    now,
		CorrelationID: askID,
		CausationID:   strings.TrimSpace(msg.RequestID),
		ActorID:       strings.TrimSpace(req.AgentID),
		RequestID:     strings.TrimSpace(msg.RequestID),
		Command:       "ask",
		Payload: events.MustMarshalPayload(map[string]any{
			"ask_id":    askID,
			"kind":      strings.TrimSpace(msg.Kind),
			"question":  strings.TrimSpace(msg.Question),
			"agent_id":  strings.TrimSpace(req.AgentID),
			"namespace": strings.TrimSpace(msg.ToNS),
			"status":    "failed",
			"error":     strings.TrimSpace(dispatchErr.Error()),
			"failed_at": now.UTC().Format(time.RFC3339Nano),
		}),
	}
	if err := s.deps.Events.Append(ctx, evt); err != nil {
		return asDependencyError("append ask failure event", err)
	}
	if s.deps.Projections != nil {
		if err := s.deps.Projections.Apply(ctx, evt); err != nil {
			return asDependencyError("apply ask failure projection", err)
		}
	}
	return nil
}
