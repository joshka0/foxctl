package services

import (
	"context"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/core/ask"
	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
)

// AskDependencies wires AskService collaborators.
type AskDependencies struct {
	Policy     AskPolicyAuthorizer
	Dispatcher AskDispatcher
	DefaultTTL time.Duration
	NewID      func() string
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

	messageID, err := s.deps.Dispatcher.Send(ctx, msg)
	if err != nil {
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
