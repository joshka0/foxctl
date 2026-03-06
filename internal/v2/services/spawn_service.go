package services

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"
	"time"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
)

// SpawnDependencies wires SpawnService collaborators.
type SpawnDependencies struct {
	RunService     RunInvoker
	RuntimeSpawner RuntimeSpawner
	Events         EventStreamReader
	Projections    ProjectionStore
	Now            func() time.Time
	NewID          func() string
}

// RuntimeSpawner performs parent-aware child spawn against an external runtime.
type RuntimeSpawner interface {
	SpawnChild(ctx context.Context, req spawn.Request) (spawn.Response, error)
}

// SpawnService orchestrates spawn operations through the shared run service.
type SpawnService struct {
	deps SpawnDependencies
	mu   sync.Mutex

	runtimeDedupe map[string]spawn.Response
}

// NewSpawnService builds a spawn service with deterministic-friendly defaults.
func NewSpawnService(deps SpawnDependencies) *SpawnService {
	if deps.Now == nil {
		deps.Now = defaultNow()
	}
	if deps.NewID == nil {
		deps.NewID = defaultNewID()
	}
	return &SpawnService{
		deps:          deps,
		runtimeDedupe: map[string]spawn.Response{},
	}
}

// Spawn validates request, enforces request-id idempotency, executes a run turn,
// and hydrates projections from append-only events.
func (s *SpawnService) Spawn(ctx context.Context, req spawn.Request) (spawn.Response, error) {
	if s == nil {
		return spawn.Response{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "spawn service is not configured",
			Fatal:   true,
		}
	}

	req.Role = strings.TrimSpace(req.Role)
	if req.Role == "" {
		return spawn.Response{}, asValidationError("role is required", map[string]any{
			"field": "role",
		})
	}

	req.RequestID = normalizeOrGenerate(req.RequestID, defaultRequestPref, s.deps.NewID)
	req.ParentAgentID = strings.TrimSpace(req.ParentAgentID)

	// Idempotency key: request_id.
	if s.deps.Projections != nil && req.RequestID != "" {
		state, err := s.deps.Projections.GetRunStateByRequestID(ctx, req.RequestID)
		switch {
		case err == nil:
			return spawn.Response{
				RunID:      state.RunID,
				AgentID:    normalizeOrGenerate(req.AgentID, defaultAgentPref, s.deps.NewID),
				ActorID:    state.ActorID,
				RequestID:  req.RequestID,
				Status:     state.Status,
				Idempotent: true,
				CreatedAt:  s.deps.Now(),
			}, nil
		case isNotFound(err):
			// continue
		default:
			return spawn.Response{}, asDependencyError("lookup spawn idempotency", err)
		}
	}

	if req.ParentAgentID != "" {
		if previous, ok := s.getRuntimeDeduped(req.RequestID); ok {
			previous.Idempotent = true
			return previous, nil
		}
		if s.deps.RuntimeSpawner == nil {
			return spawn.Response{}, &v2errors.V2Error{
				Kind:    v2errors.ErrDependency,
				Message: "spawn runtime spawner is not configured",
				Fatal:   true,
				Details: map[string]any{
					"parent_agent_id": req.ParentAgentID,
				},
			}
		}
	}

	if s.deps.RunService == nil && req.ParentAgentID == "" {
		return spawn.Response{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "spawn run service is not configured",
			Fatal:   true,
		}
	}

	runID := normalizeOrGenerate(req.RunID, defaultRunPref, s.deps.NewID)
	agentID := normalizeOrGenerate(req.AgentID, defaultAgentPref, s.deps.NewID)
	actorID := normalizeOrGenerate(req.ActorID, "actor:"+req.Role, s.deps.NewID)
	req.RunID = runID
	req.AgentID = agentID
	req.ActorID = actorID

	if req.ParentAgentID != "" {
		resp, err := s.deps.RuntimeSpawner.SpawnChild(ctx, req)
		if err != nil {
			return spawn.Response{}, err
		}
		if resp.RunID == "" {
			resp.RunID = runID
		}
		if resp.AgentID == "" {
			resp.AgentID = agentID
		}
		if resp.ActorID == "" {
			resp.ActorID = actorID
		}
		if resp.RequestID == "" {
			resp.RequestID = req.RequestID
		}
		if resp.Status == "" {
			resp.Status = "spawned"
		}
		if resp.CreatedAt.IsZero() {
			resp.CreatedAt = s.deps.Now()
		}
		s.setRuntimeDeduped(req.RequestID, resp)
		return resp, nil
	}

	turnID := prefixedID(defaultTurnPref, s.deps.NewID)

	mode := strings.TrimSpace(req.ExecMode)
	if mode == "" {
		mode = defaultSpawnMode
	}
	correlationID := strings.TrimSpace(req.CorrelationID)
	if correlationID == "" {
		correlationID = req.RequestID
	}

	out, err := s.deps.RunService.Run(ctx, run.TurnInput{
		RunID:         runID,
		TurnID:        turnID,
		Command:       "spawn",
		Mode:          mode,
		Prompt:        strings.TrimSpace(req.Prompt),
		ActorID:       actorID,
		CorrelationID: correlationID,
		CausationID:   strings.TrimSpace(req.CausationID),
		RequestID:     req.RequestID,
		MaxIterations: req.MaxIterations,
	})
	if err != nil {
		return spawn.Response{}, err
	}

	if err := s.hydrateProjection(ctx, runID); err != nil {
		return spawn.Response{}, err
	}

	status := "completed"
	if s.deps.Projections != nil {
		state, stateErr := s.deps.Projections.GetRunState(ctx, runID)
		switch {
		case stateErr == nil:
			if strings.TrimSpace(state.Status) != "" {
				status = state.Status
			}
		case isNotFound(stateErr):
			// keep default
		default:
			return spawn.Response{}, asDependencyError("read run state", stateErr)
		}
	}

	return spawn.Response{
		RunID:      runID,
		AgentID:    agentID,
		ActorID:    actorID,
		TurnID:     out.TurnID,
		RequestID:  req.RequestID,
		Status:     status,
		Summary:    out.Summary,
		Iterations: out.Iterations,
		ToolCalls:  out.ToolCalls,
		Degraded:   out.Degraded,
		CreatedAt:  s.deps.Now(),
	}, nil
}

func (s *SpawnService) getRuntimeDeduped(requestID string) (spawn.Response, bool) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return spawn.Response{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	resp, ok := s.runtimeDedupe[requestID]
	return resp, ok
}

func (s *SpawnService) setRuntimeDeduped(requestID string, resp spawn.Response) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	s.mu.Lock()
	s.runtimeDedupe[requestID] = resp
	s.mu.Unlock()
}

func (s *SpawnService) hydrateProjection(ctx context.Context, runID string) error {
	if s.deps.Events == nil || s.deps.Projections == nil {
		return nil
	}
	eventsList, err := s.deps.Events.ListStream(ctx, events.StreamFilter{
		StreamID:   runID,
		StreamType: events.StreamTypeRun,
	})
	if err != nil {
		return asDependencyError("list run stream events", err)
	}
	for _, evt := range eventsList {
		if applyErr := s.deps.Projections.Apply(ctx, evt); applyErr != nil {
			return asDependencyError("apply run projection", applyErr)
		}
	}
	return nil
}

func asPolicyError(err error) error {
	if err == nil {
		return nil
	}
	var verr *v2errors.V2Error
	if stderrors.As(err, &verr) {
		return verr
	}
	return &v2errors.V2Error{
		Kind:    v2errors.ErrPolicyViolation,
		Message: "request blocked by policy",
		Cause:   err,
		Fatal:   true,
	}
}

func asKillError(runID string, err error) error {
	if err == nil {
		return nil
	}
	var verr *v2errors.V2Error
	if stderrors.As(err, &verr) {
		return verr
	}
	if isNotFound(err) {
		return asNotFoundError("run not found", map[string]any{
			"run_id": runID,
		})
	}
	return &v2errors.V2Error{
		Kind:      v2errors.ErrDependency,
		Message:   "kill operation failed",
		Cause:     err,
		Fatal:     true,
		Retryable: true,
		Details: map[string]any{
			"run_id": runID,
		},
	}
}

func asProjectionListError(err error) error {
	if err == nil {
		return nil
	}
	var verr *v2errors.V2Error
	if stderrors.As(err, &verr) {
		return verr
	}
	return &v2errors.V2Error{
		Kind:      v2errors.ErrDependency,
		Message:   "list projected runs failed",
		Cause:     err,
		Fatal:     true,
		Retryable: true,
	}
}

func parsePositiveDurationOrDefault(d time.Duration, fallback time.Duration) time.Duration {
	if d <= 0 {
		return fallback
	}
	return d
}

func requestToCallerNS(value string, newID func() string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fmt.Sprintf("%s:caller:%s", defaultCallerPref, strings.TrimSpace(newID()))
}
