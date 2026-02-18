package services

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/core/ask"
	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

const (
	defaultListLimit   = 20
	maxListLimit       = 200
	defaultAskKind     = "context"
	defaultSpawnMode   = "reactive"
	defaultRequestPref = "req"
	defaultRunPref     = "run"
	defaultTurnPref    = "turn"
	defaultAgentPref   = "agent"
	defaultAskPref     = "ask"
	defaultCallerPref  = "v2"
)

// TurnRunner executes one canonical turn.
type TurnRunner interface {
	RunTurn(ctx context.Context, in run.TurnInput) (run.TurnOutput, error)
}

// RunInvoker is a service-level wrapper for turn execution.
type RunInvoker interface {
	Run(ctx context.Context, in run.TurnInput) (run.TurnOutput, error)
}

// EventStreamReader reads ordered stream events for projection hydration.
type EventStreamReader interface {
	ListStream(ctx context.Context, filter events.StreamFilter) ([]events.Event, error)
}

// RunState is the service-facing projection read model.
type RunState struct {
	RunID     string
	Status    string
	Command   string
	RequestID string
	ActorID   string
	UpdatedAt time.Time
}

// RunStateFilter is the service-facing projection list filter.
type RunStateFilter struct {
	Limit   int
	Status  string
	Command string
	ActorID string
}

// ProjectionStore materializes and reads run projections.
type ProjectionStore interface {
	Apply(ctx context.Context, evt events.Event) error
	GetRunState(ctx context.Context, runID string) (RunState, error)
	GetRunStateByRequestID(ctx context.Context, requestID string) (RunState, error)
	ListRunStates(ctx context.Context, filter RunStateFilter) ([]RunState, error)
}

// IDMapStore resolves legacy IDs to v2 IDs and writes immutable mappings.
type IDMapStore interface {
	Put(ctx context.Context, entityType, legacyID, v2ID string) error
	ResolveV2ID(ctx context.Context, entityType, legacyID string) (string, error)
}

// AskPolicyAuthorizer gates ask requests by policy.
type AskPolicyAuthorizer interface {
	AuthorizeAsk(ctx context.Context, req ask.Request) error
}

// AskDispatcher sends normalized ask messages to the runtime transport.
type AskDispatcher interface {
	Send(ctx context.Context, msg ask.Message) (string, error)
}

// RunKiller terminates a run/session by resolved v2 run ID.
type RunKiller interface {
	Kill(ctx context.Context, runID string) error
}

func defaultNow() func() time.Time {
	return func() time.Time { return time.Now().UTC() }
}

func defaultNewID() func() string {
	var seq atomic.Uint64
	return func() string {
		n := seq.Add(1)
		return fmt.Sprintf("id-%06d", n)
	}
}

var defaultIDGenerator = defaultNewID()

func prefixedID(prefix string, nextID func() string) string {
	if nextID == nil {
		nextID = defaultIDGenerator
	}
	return strings.TrimSpace(prefix) + "-" + strings.TrimSpace(nextID())
}

func normalizeOrGenerate(value, prefix string, nextID func() string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return prefixedID(prefix, nextID)
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return stderrors.Is(err, events.ErrNotFound)
}

func asDependencyError(message string, err error) *v2errors.V2Error {
	if err == nil {
		return nil
	}
	var verr *v2errors.V2Error
	if stderrors.As(err, &verr) {
		return verr
	}
	return &v2errors.V2Error{
		Kind:      v2errors.ErrDependency,
		Message:   message,
		Cause:     err,
		Fatal:     true,
		Retryable: true,
	}
}

func asValidationError(message string, details map[string]any) *v2errors.V2Error {
	return &v2errors.V2Error{
		Kind:    v2errors.ErrValidation,
		Message: message,
		Fatal:   true,
		Details: details,
	}
}

func asNotFoundError(message string, details map[string]any) *v2errors.V2Error {
	return &v2errors.V2Error{
		Kind:    v2errors.ErrNotFound,
		Message: message,
		Fatal:   true,
		Details: details,
	}
}
