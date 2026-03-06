package services

import (
	"context"
	stderrors "errors"
	"strings"
	"sync"
	"time"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/orchestration"
	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
)

const (
	defaultBoardLimit = 50
	maxBoardLimit     = 200

	commandDispatchIssue = "orchestration/dispatch-issue"
	commandRefresh       = "orchestration/refresh"
	defaultDispatchRole  = "coder"
)

// OrchestrationSpawner is the canonical spawn dependency used for issue dispatch.
type OrchestrationSpawner interface {
	Spawn(ctx context.Context, req spawn.Request) (spawn.Response, error)
}

// OrchestrationReader reads projection-backed board/card views.
type OrchestrationReader interface {
	Board(ctx context.Context, req orchestration.BoardRequest) (orchestration.BoardResponse, error)
	Card(ctx context.Context, req orchestration.CardRequest) (orchestration.CardResponse, error)
}

// OrchestrationRefreshQueue queues immediate reconcile/poll work.
type OrchestrationRefreshQueue interface {
	Enqueue(ctx context.Context, workspaceID, requestID string) (queued bool, coalesced bool, err error)
}

// OrchestrationDependencies wires orchestration service collaborators.
type OrchestrationDependencies struct {
	Spawn        OrchestrationSpawner
	Reader       OrchestrationReader
	RefreshQueue OrchestrationRefreshQueue
	LaneOptions  orchestration.LaneOptions
	Now          func() time.Time
}

// OrchestrationService handles dispatch, board, card, and refresh commands.
type OrchestrationService struct {
	deps OrchestrationDependencies

	mu             sync.Mutex
	dispatchDedupe map[string]orchestration.DispatchResponse
	refreshDedupe  map[string]orchestration.RefreshResponse
}

// NewOrchestrationService builds an orchestration service.
func NewOrchestrationService(deps OrchestrationDependencies) *OrchestrationService {
	if deps.Now == nil {
		deps.Now = defaultNow()
	}
	return &OrchestrationService{
		deps:           deps,
		dispatchDedupe: map[string]orchestration.DispatchResponse{},
		refreshDedupe:  map[string]orchestration.RefreshResponse{},
	}
}

// DispatchIssue routes issue dispatch through SpawnService.Spawn only.
func (s *OrchestrationService) DispatchIssue(ctx context.Context, req orchestration.DispatchRequest) (orchestration.DispatchResponse, error) {
	if s == nil || s.deps.Spawn == nil {
		return orchestration.DispatchResponse{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "orchestration spawn dependency is not configured",
			Fatal:   true,
		}
	}

	req.RequestID = strings.TrimSpace(req.RequestID)
	if req.RequestID == "" {
		return orchestration.DispatchResponse{}, asValidationError("request_id is required", map[string]any{
			"field": "request_id",
		})
	}

	req.IssueID = strings.TrimSpace(req.IssueID)
	if req.IssueID == "" {
		return orchestration.DispatchResponse{}, asValidationError("issue_id is required", map[string]any{
			"field": "issue_id",
		})
	}

	key := idempotencyKey(commandDispatchIssue, req.IssueID, req.RequestID)
	if previous, ok := s.getDispatchDeduped(key); ok {
		previous.Idempotent = true
		return previous, nil
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = defaultDispatchRole
	}

	spawnResp, err := s.deps.Spawn.Spawn(ctx, spawn.Request{
		RequestID:     req.RequestID,
		Role:          role,
		Prompt:        strings.TrimSpace(req.Prompt),
		ExecMode:      strings.TrimSpace(req.ExecMode),
		RunID:         strings.TrimSpace(req.RunID),
		AgentID:       strings.TrimSpace(req.AgentID),
		ActorID:       strings.TrimSpace(req.ActorID),
		ParentAgentID: strings.TrimSpace(req.ParentAgentID),
		Metadata:      dispatchSpawnMetadata(req),

		CorrelationID: strings.TrimSpace(req.CorrelationID),
		CausationID:   strings.TrimSpace(req.CausationID),

		MaxIterations:    req.MaxIterations,
		MaxContextTokens: req.MaxContextTokens,
		MaxAutoTurns:     req.MaxAutoTurns,
		ThinkInterval:    req.ThinkInterval,
	})
	if err != nil {
		var verr *v2errors.V2Error
		if stderrors.As(err, &verr) && verr.Kind == v2errors.ErrPolicyViolation {
			denialReason := strings.TrimSpace(verr.Message)
			if denialReason == "" {
				denialReason = verr.Error()
			}
			resp := orchestration.DispatchResponse{
				RequestID:       req.RequestID,
				WorkspaceID:     strings.TrimSpace(req.WorkspaceID),
				IssueID:         req.IssueID,
				IssueIdentifier: strings.TrimSpace(req.IssueIdentifier),
				Status:          "blocked",
				PolicyStatus:    orchestration.PolicyStatusDenied,
				LastOutcome:     orchestration.OutcomePolicyDenied,
				DenialReason:    denialReason,
				Suggestion:      detailString(verr.Details, "suggestion"),
				Timestamp:       s.deps.Now(),
			}
			s.setDispatchDeduped(key, resp)
			return resp, nil
		}
		return orchestration.DispatchResponse{}, asDependencyError("dispatch issue spawn", err)
	}

	resp := orchestration.DispatchResponse{
		RequestID:       req.RequestID,
		WorkspaceID:     strings.TrimSpace(req.WorkspaceID),
		IssueID:         req.IssueID,
		IssueIdentifier: strings.TrimSpace(req.IssueIdentifier),
		Status:          "dispatched",
		PolicyStatus:    orchestration.PolicyStatusOK,
		RunID:           spawnResp.RunID,
		TurnID:          spawnResp.TurnID,
		AgentID:         spawnResp.AgentID,
		ActorID:         spawnResp.ActorID,
		Timestamp:       s.deps.Now(),
	}
	s.setDispatchDeduped(key, resp)
	return resp, nil
}

func dispatchSpawnMetadata(req orchestration.DispatchRequest) map[string]any {
	meta := map[string]any{}
	putSpawnMeta(meta, "workspace_id", req.WorkspaceID)
	putSpawnMeta(meta, "issue_id", req.IssueID)
	putSpawnMeta(meta, "issue_identifier", req.IssueIdentifier)
	putSpawnMeta(meta, "title", req.Title)
	if req.Attempt > 0 {
		meta["attempt"] = req.Attempt
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func putSpawnMeta(dst map[string]any, key, value string) {
	if dst == nil {
		return
	}
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		dst[key] = trimmed
	}
}

// Board returns the projection-backed board view with bounded deterministic filters.
func (s *OrchestrationService) Board(ctx context.Context, req orchestration.BoardRequest) (orchestration.BoardResponse, error) {
	if s == nil || s.deps.Reader == nil {
		return orchestration.BoardResponse{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "orchestration reader is not configured",
			Fatal:   true,
		}
	}

	req.Limit = normalizeBoardLimit(req.Limit)
	resp, err := s.deps.Reader.Board(ctx, req)
	if err != nil {
		if isNotFound(err) {
			return orchestration.BoardResponse{}, asNotFoundError("orchestration board not found", nil)
		}
		return orchestration.BoardResponse{}, asDependencyError("read orchestration board", err)
	}
	if resp.GeneratedAt.IsZero() {
		resp.GeneratedAt = s.deps.Now()
	}
	resp.Counts = orchestration.EnsureLaneCounts(resp.Counts)
	return resp, nil
}

// Card returns one projection-backed issue card.
func (s *OrchestrationService) Card(ctx context.Context, req orchestration.CardRequest) (orchestration.CardResponse, error) {
	if s == nil || s.deps.Reader == nil {
		return orchestration.CardResponse{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "orchestration reader is not configured",
			Fatal:   true,
		}
	}

	req.IssueID = strings.TrimSpace(req.IssueID)
	if req.IssueID == "" {
		return orchestration.CardResponse{}, asValidationError("issue_id is required", map[string]any{
			"field": "issue_id",
		})
	}

	resp, err := s.deps.Reader.Card(ctx, req)
	if err != nil {
		if isNotFound(err) {
			return orchestration.CardResponse{}, asNotFoundError("orchestration card not found", map[string]any{
				"issue_id": req.IssueID,
			})
		}
		return orchestration.CardResponse{}, asDependencyError("read orchestration card", err)
	}
	if resp.Card.Lane == "" {
		resp.Card.Lane = orchestration.DeriveLane(resp.Card, s.deps.LaneOptions)
	}
	return resp, nil
}

// Refresh enqueues immediate poll/reconcile and enforces request-id idempotency.
func (s *OrchestrationService) Refresh(ctx context.Context, req orchestration.RefreshRequest) (orchestration.RefreshResponse, error) {
	if s == nil || s.deps.RefreshQueue == nil {
		return orchestration.RefreshResponse{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "orchestration refresh queue is not configured",
			Fatal:   true,
		}
	}

	req.RequestID = strings.TrimSpace(req.RequestID)
	if req.RequestID == "" {
		return orchestration.RefreshResponse{}, asValidationError("request_id is required", map[string]any{
			"field": "request_id",
		})
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	key := idempotencyKey(commandRefresh, workspaceID, req.RequestID)
	if previous, ok := s.getRefreshDeduped(key); ok {
		previous.Idempotent = true
		return previous, nil
	}

	queued, coalesced, err := s.deps.RefreshQueue.Enqueue(ctx, workspaceID, req.RequestID)
	if err != nil {
		return orchestration.RefreshResponse{}, asDependencyError("enqueue orchestration refresh", err)
	}

	resp := orchestration.RefreshResponse{
		RequestID: req.RequestID,
		Queued:    queued,
		Coalesced: coalesced,
		Timestamp: s.deps.Now(),
	}
	s.setRefreshDeduped(key, resp)
	return resp, nil
}

func normalizeBoardLimit(limit int) int {
	if limit <= 0 {
		return defaultBoardLimit
	}
	if limit > maxBoardLimit {
		return maxBoardLimit
	}
	return limit
}

func idempotencyKey(command, scopeID, requestID string) string {
	return strings.TrimSpace(command) + "|" + strings.TrimSpace(scopeID) + "|" + strings.TrimSpace(requestID)
}

func detailString(details map[string]any, key string) string {
	if len(details) == 0 {
		return ""
	}
	raw, ok := details[key]
	if !ok || raw == nil {
		return ""
	}
	if asString, ok := raw.(string); ok {
		return strings.TrimSpace(asString)
	}
	return ""
}

func (s *OrchestrationService) getDispatchDeduped(key string) (orchestration.DispatchResponse, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp, ok := s.dispatchDedupe[key]
	return resp, ok
}

func (s *OrchestrationService) setDispatchDeduped(key string, value orchestration.DispatchResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dispatchDedupe[key] = value
}

func (s *OrchestrationService) getRefreshDeduped(key string) (orchestration.RefreshResponse, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp, ok := s.refreshDedupe[key]
	return resp, ok
}

func (s *OrchestrationService) setRefreshDeduped(key string, value orchestration.RefreshResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshDedupe[key] = value
}
