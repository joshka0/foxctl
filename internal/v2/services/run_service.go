package services

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"strings"
	"sync"
	"time"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

const defaultTurnRequestStaleAfter = 30 * time.Minute

// RunServiceConfig contains narrow runtime knobs for RunService behavior.
type RunServiceConfig struct {
	// TurnRequestStaleAfter controls when an existing running turn request may
	// be reclaimed. Zero or negative disables stale recovery.
	TurnRequestStaleAfter time.Duration
}

// RunService executes the canonical v2 turn pipeline.
type RunService struct {
	runner                TurnRunner
	registry              run.TurnRequestRegistry
	now                   func() time.Time
	turnRequestStaleAfter time.Duration
}

// NewRunService builds a run service.
func NewRunService(runner TurnRunner) *RunService {
	return NewRunServiceWithRegistry(runner, nil, nil)
}

// NewRunServiceWithRegistry builds a run service with durable request idempotency.
func NewRunServiceWithRegistry(runner TurnRunner, registry run.TurnRequestRegistry, now func() time.Time) *RunService {
	return newRunServiceWithRegistry(runner, registry, now, defaultTurnRequestStaleAfter)
}

// NewRunServiceWithRegistryConfig builds a run service with explicit durable
// request idempotency configuration.
func NewRunServiceWithRegistryConfig(
	runner TurnRunner,
	registry run.TurnRequestRegistry,
	now func() time.Time,
	cfg RunServiceConfig,
) *RunService {
	return newRunServiceWithRegistry(runner, registry, now, cfg.TurnRequestStaleAfter)
}

func newRunServiceWithRegistry(
	runner TurnRunner,
	registry run.TurnRequestRegistry,
	now func() time.Time,
	turnRequestStaleAfter time.Duration,
) *RunService {
	if now == nil {
		now = defaultNow()
	}
	return &RunService{
		runner:                runner,
		registry:              registry,
		now:                   now,
		turnRequestStaleAfter: turnRequestStaleAfter,
	}
}

// Run validates input and executes one canonical run turn.
func (s *RunService) Run(ctx context.Context, in run.TurnInput) (run.TurnOutput, error) {
	in.RunID = strings.TrimSpace(in.RunID)
	if in.RunID == "" {
		return run.TurnOutput{}, asValidationError("run_id is required", map[string]any{
			"field": "run_id",
		})
	}
	if in.MaxIterations < 0 {
		return run.TurnOutput{}, asValidationError("max_iterations must be >= 0", map[string]any{
			"field": "max_iterations",
		})
	}
	if strings.TrimSpace(in.Command) == "" {
		in.Command = "run"
	}

	if s == nil || s.runner == nil {
		return run.TurnOutput{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "turn runner is not configured",
			Fatal:   true,
		}
	}

	registry := s.registry
	if registry != nil {
		in.TurnID = strings.TrimSpace(in.TurnID)
		in.RequestID = strings.TrimSpace(in.RequestID)
		if in.TurnID == "" {
			return run.TurnOutput{}, asValidationError("turn_id is required for durable run requests", map[string]any{
				"field": "turn_id",
			})
		}
		if in.RequestID == "" {
			return run.TurnOutput{}, asValidationError("request_id is required for durable run requests", map[string]any{
				"field": "request_id",
			})
		}

		now := s.now().UTC()
		existing, inserted, err := registry.BeginTurnRequest(ctx, run.TurnRequestRecord{
			RunID:     in.RunID,
			RequestID: in.RequestID,
			TurnID:    in.TurnID,
			Status:    run.TurnRequestRunning,
			StartedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			return run.TurnOutput{TurnID: in.TurnID}, asTurnRequestError("begin turn request", err, in, existing)
		}
		if !inserted {
			var recoverErr *v2errors.V2Error
			existing, inserted, recoverErr = s.recoverStaleTurnRequest(ctx, registry, in, existing, now)
			if recoverErr != nil {
				return run.TurnOutput{TurnID: in.TurnID}, recoverErr
			}
			if !inserted {
				return turnRequestDuplicateResult(existing)
			}
		}
	}

	stopHeartbeat := s.startTurnRequestHeartbeat(ctx, registry, in)
	defer stopHeartbeat()

	out, err := s.runner.RunTurn(ctx, in)
	if err == nil {
		if registry != nil {
			record := successfulTurnRequest(in, out, s.now().UTC())
			completed, completeErr := registry.CompleteTurnRequest(ctx, record)
			if completeErr != nil {
				return out, asTurnRequestError("complete turn request", completeErr, in, run.TurnRequestRecord{})
			}
			if !turnRequestCompletionMatches(completed, record) {
				return turnRequestDuplicateResult(completed)
			}
		}
		return out, nil
	}

	var verr *v2errors.V2Error
	if stderrors.As(err, &verr) {
		if registry != nil {
			record := failedTurnRequest(in, out, verr, s.now().UTC())
			completed, completeErr := registry.CompleteTurnRequest(ctx, record)
			if completeErr != nil {
				return out, asTurnRequestError("complete failed turn request", completeErr, in, run.TurnRequestRecord{})
			}
			if !turnRequestCompletionMatches(completed, record) {
				return turnRequestDuplicateResult(completed)
			}
		}
		return out, verr
	}
	verr = &v2errors.V2Error{
		Kind:      v2errors.ErrInternal,
		Message:   "run execution failed",
		Cause:     err,
		Fatal:     true,
		Retryable: false,
	}
	if registry != nil {
		record := failedTurnRequest(in, out, verr, s.now().UTC())
		completed, completeErr := registry.CompleteTurnRequest(ctx, record)
		if completeErr != nil {
			return out, asTurnRequestError("complete failed turn request", completeErr, in, run.TurnRequestRecord{})
		}
		if !turnRequestCompletionMatches(completed, record) {
			return turnRequestDuplicateResult(completed)
		}
	}
	return out, verr
}

func (s *RunService) recoverStaleTurnRequest(
	ctx context.Context,
	registry run.TurnRequestRegistry,
	in run.TurnInput,
	existing run.TurnRequestRecord,
	now time.Time,
) (run.TurnRequestRecord, bool, *v2errors.V2Error) {
	if existing.Status != run.TurnRequestRunning {
		return existing, false, nil
	}
	if s.turnRequestStaleAfter <= 0 {
		return existing, false, nil
	}
	lastActivity := turnRequestLastActivity(existing)
	if lastActivity.IsZero() || !lastActivity.Before(now.Add(-s.turnRequestStaleAfter)) {
		return existing, false, nil
	}
	recoverer, ok := registry.(run.StaleTurnRequestRecoverer)
	if !ok {
		return existing, false, nil
	}
	record, recovered, err := recoverer.RecoverStaleTurnRequest(ctx, run.TurnRequestRecord{
		RunID:     in.RunID,
		RequestID: in.RequestID,
		TurnID:    in.TurnID,
		Status:    run.TurnRequestRunning,
		StartedAt: now,
		UpdatedAt: now,
	}, now.Add(-s.turnRequestStaleAfter))
	if err != nil {
		return existing, false, asTurnRequestError("recover stale turn request", err, in, record)
	}
	return record, recovered, nil
}

func (s *RunService) startTurnRequestHeartbeat(ctx context.Context, registry run.TurnRequestRegistry, in run.TurnInput) func() {
	if registry == nil || s.turnRequestStaleAfter <= 0 {
		return func() {}
	}
	toucher, ok := registry.(run.TurnRequestToucher)
	if !ok {
		return func() {}
	}
	interval := turnRequestHeartbeatInterval(s.turnRequestStaleAfter)
	if interval <= 0 {
		return func() {}
	}

	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, touched, err := toucher.TouchTurnRequest(ctx, in.RunID, in.RequestID, in.TurnID, s.now().UTC())
				if err != nil || !touched {
					return
				}
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()
	return func() {
		once.Do(func() { close(done) })
	}
}

func turnRequestHeartbeatInterval(staleAfter time.Duration) time.Duration {
	if staleAfter <= 0 {
		return 0
	}
	interval := staleAfter / 3
	if interval < 10*time.Millisecond {
		return 10 * time.Millisecond
	}
	if interval > 5*time.Minute {
		return 5 * time.Minute
	}
	return interval
}

func turnRequestLastActivity(rec run.TurnRequestRecord) time.Time {
	if !rec.UpdatedAt.IsZero() {
		return rec.UpdatedAt.UTC()
	}
	if !rec.StartedAt.IsZero() {
		return rec.StartedAt.UTC()
	}
	return time.Time{}
}

func turnRequestCompletionMatches(got, want run.TurnRequestRecord) bool {
	return got.Status == want.Status &&
		bytes.Equal(got.OutputJSON, want.OutputJSON) &&
		bytes.Equal(got.ErrorJSON, want.ErrorJSON)
}

func successfulTurnRequest(in run.TurnInput, out run.TurnOutput, now time.Time) run.TurnRequestRecord {
	raw, err := json.Marshal(out)
	if err != nil {
		raw = nil
	}
	return run.TurnRequestRecord{
		RunID:       in.RunID,
		RequestID:   in.RequestID,
		TurnID:      in.TurnID,
		Status:      run.TurnRequestSucceeded,
		OutputJSON:  raw,
		CompletedAt: now,
		UpdatedAt:   now,
	}
}

func failedTurnRequest(in run.TurnInput, out run.TurnOutput, verr *v2errors.V2Error, now time.Time) run.TurnRequestRecord {
	if verr == nil {
		verr = &v2errors.V2Error{Kind: v2errors.ErrInternal, Message: "run execution failed", Fatal: true}
	}
	status := run.TurnRequestFailed
	if verr.Kind == v2errors.ErrTimeout {
		status = run.TurnRequestCanceled
	}
	raw, err := json.Marshal(turnRequestErrorJSON{
		Kind:      string(verr.Kind),
		Message:   verr.Message,
		Fatal:     verr.Fatal,
		Retryable: verr.Retryable,
		Details:   verr.Details,
	})
	if err != nil {
		raw = nil
	}
	if out.TurnID != "" && in.TurnID == "" {
		in.TurnID = out.TurnID
	}
	return run.TurnRequestRecord{
		RunID:       in.RunID,
		RequestID:   in.RequestID,
		TurnID:      in.TurnID,
		Status:      status,
		ErrorJSON:   raw,
		CompletedAt: now,
		UpdatedAt:   now,
	}
}

type turnRequestErrorJSON struct {
	Kind      string         `json:"kind,omitempty"`
	Message   string         `json:"message,omitempty"`
	Fatal     bool           `json:"fatal,omitempty"`
	Retryable bool           `json:"retryable,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

func turnRequestDuplicateResult(rec run.TurnRequestRecord) (run.TurnOutput, error) {
	rec = rec.Clone()
	switch rec.Status {
	case run.TurnRequestSucceeded:
		var out run.TurnOutput
		if len(rec.OutputJSON) > 0 {
			if err := json.Unmarshal(rec.OutputJSON, &out); err != nil {
				return run.TurnOutput{TurnID: rec.TurnID}, &v2errors.V2Error{
					Kind:    v2errors.ErrDependency,
					Message: "decode stored turn request output",
					Cause:   err,
					Fatal:   true,
				}
			}
		}
		if out.TurnID == "" {
			out.TurnID = rec.TurnID
		}
		return out, nil
	case run.TurnRequestFailed, run.TurnRequestCanceled:
		return run.TurnOutput{TurnID: rec.TurnID}, decodeTurnRequestError(rec)
	default:
		return run.TurnOutput{TurnID: rec.TurnID}, turnRequestRunningError(rec)
	}
}

func decodeTurnRequestError(rec run.TurnRequestRecord) error {
	if len(rec.ErrorJSON) == 0 {
		return &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "turn request previously failed",
			Fatal:   true,
			Details: turnRequestDetails(rec),
		}
	}
	var stored turnRequestErrorJSON
	if err := json.Unmarshal(rec.ErrorJSON, &stored); err != nil {
		return &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "decode stored turn request error",
			Cause:   err,
			Fatal:   true,
			Details: turnRequestDetails(rec),
		}
	}
	kind := v2errors.ErrorKind(strings.TrimSpace(stored.Kind))
	if kind == "" {
		kind = v2errors.ErrInternal
	}
	msg := strings.TrimSpace(stored.Message)
	if msg == "" {
		msg = "turn request previously failed"
	}
	return &v2errors.V2Error{
		Kind:      kind,
		Message:   msg,
		Fatal:     stored.Fatal,
		Retryable: stored.Retryable,
		Details:   stored.Details,
	}
}

func turnRequestRunningError(rec run.TurnRequestRecord) *v2errors.V2Error {
	return &v2errors.V2Error{
		Kind:      v2errors.ErrConflict,
		Message:   "turn request is already running",
		Fatal:     true,
		Retryable: true,
		Details:   turnRequestDetails(rec),
	}
}

func asTurnRequestError(message string, err error, in run.TurnInput, rec run.TurnRequestRecord) *v2errors.V2Error {
	if err == nil {
		return nil
	}
	if stderrors.Is(err, run.ErrTurnRequestConflict) {
		if rec.RunID == "" {
			rec.RunID = in.RunID
		}
		if rec.RequestID == "" {
			rec.RequestID = in.RequestID
		}
		if rec.TurnID == "" {
			rec.TurnID = in.TurnID
		}
		return &v2errors.V2Error{
			Kind:    v2errors.ErrConflict,
			Message: message,
			Cause:   err,
			Fatal:   true,
			Details: turnRequestDetails(rec),
		}
	}
	return asDependencyError(message, err)
}

func turnRequestDetails(rec run.TurnRequestRecord) map[string]any {
	return map[string]any{
		"run_id":     rec.RunID,
		"request_id": rec.RequestID,
		"turn_id":    rec.TurnID,
		"status":     string(rec.Status),
	}
}
