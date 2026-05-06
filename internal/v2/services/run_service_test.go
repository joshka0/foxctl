package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func TestRunService_WithoutRegistryPreservesRunnerBehavior(t *testing.T) {
	t.Parallel()

	runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "turn-plain", Summary: "ok"}}
	svc := NewRunService(runner)

	out, err := svc.Run(context.Background(), run.TurnInput{RunID: "run-plain"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.TurnID != "turn-plain" {
		t.Fatalf("turn_id=%q want turn-plain", out.TurnID)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls=%d want 1", runner.calls)
	}
}

func TestRunService_WithRegistryRejectsMissingIdentityBeforeRunner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   run.TurnInput
	}{
		{name: "missing turn id", in: run.TurnInput{RunID: "run-id", RequestID: "req-id"}},
		{name: "missing request id", in: run.TurnInput{RunID: "run-id", TurnID: "turn-id"}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "should-not-run"}}
			svc := NewRunServiceWithRegistry(runner, newMemoryTurnRequestRegistry(), fixedRunServiceNow())

			_, err := svc.Run(context.Background(), tc.in)
			if err == nil {
				t.Fatal("Run() succeeded, want validation error")
			}
			var verr *v2errors.V2Error
			if !errors.As(err, &verr) {
				t.Fatalf("error type=%T want *V2Error", err)
			}
			if verr.Kind != v2errors.ErrValidation {
				t.Fatalf("kind=%q want %q", verr.Kind, v2errors.ErrValidation)
			}
			if runner.calls != 0 {
				t.Fatalf("runner calls=%d want 0", runner.calls)
			}
		})
	}
}

func TestRunService_DuplicateRunningRequestDoesNotCallRunner(t *testing.T) {
	t.Parallel()

	registry := newMemoryTurnRequestRegistry()
	registry.seed(run.TurnRequestRecord{
		RunID:     "run-dupe",
		RequestID: "req-dupe",
		TurnID:    "turn-dupe",
		Status:    run.TurnRequestRunning,
		StartedAt: time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC),
	})
	runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "should-not-run"}}
	svc := NewRunServiceWithRegistry(runner, registry, fixedRunServiceNow())

	out, err := svc.Run(context.Background(), run.TurnInput{
		RunID:     "run-dupe",
		TurnID:    "turn-dupe",
		RequestID: "req-dupe",
	})
	if err == nil {
		t.Fatal("Run() succeeded, want conflict")
	}
	var verr *v2errors.V2Error
	if !errors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrConflict {
		t.Fatalf("kind=%q want %q", verr.Kind, v2errors.ErrConflict)
	}
	if out.TurnID != "turn-dupe" {
		t.Fatalf("turn_id=%q want turn-dupe", out.TurnID)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls=%d want 0", runner.calls)
	}
}

func TestRunService_DefaultStaleWindowRecoversRunningRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)
	registry := newMemoryTurnRequestRegistry()
	registry.seed(run.TurnRequestRecord{
		RunID:     "run-default-stale",
		RequestID: "req-default-stale",
		TurnID:    "turn-default-stale",
		Status:    run.TurnRequestRunning,
		StartedAt: now.Add(-31 * time.Minute),
		UpdatedAt: now.Add(-31 * time.Minute),
	})
	runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "turn-default-stale", Summary: "default recovered"}}
	svc := NewRunServiceWithRegistry(runner, registry, func() time.Time { return now })

	out, err := svc.Run(context.Background(), run.TurnInput{
		RunID:     "run-default-stale",
		TurnID:    "turn-default-stale",
		RequestID: "req-default-stale",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Summary != "default recovered" {
		t.Fatalf("summary=%q want default recovered", out.Summary)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls=%d want 1", runner.calls)
	}
}

func TestRunService_StaleRunningRequestIsRecoveredAndExecutes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)
	registry := newMemoryTurnRequestRegistry()
	registry.seed(run.TurnRequestRecord{
		RunID:     "run-stale",
		RequestID: "req-stale",
		TurnID:    "turn-stale",
		Status:    run.TurnRequestRunning,
		StartedAt: now.Add(-2 * time.Minute),
		UpdatedAt: now.Add(-2 * time.Minute),
	})
	runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "turn-stale", Summary: "recovered"}}
	svc := NewRunServiceWithRegistryConfig(runner, registry, func() time.Time { return now }, RunServiceConfig{
		TurnRequestStaleAfter: time.Minute,
	})

	out, err := svc.Run(context.Background(), run.TurnInput{
		RunID:     "run-stale",
		TurnID:    "turn-stale",
		RequestID: "req-stale",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Summary != "recovered" {
		t.Fatalf("summary=%q want recovered", out.Summary)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls=%d want 1", runner.calls)
	}
	rec, err := registry.GetTurnRequest(context.Background(), "run-stale", "req-stale")
	if err != nil {
		t.Fatalf("GetTurnRequest() error = %v", err)
	}
	if rec.Status != run.TurnRequestSucceeded {
		t.Fatalf("status=%q want %q", rec.Status, run.TurnRequestSucceeded)
	}
	if !rec.StartedAt.Equal(now) {
		t.Fatalf("started_at=%s want recovered start %s", rec.StartedAt, now)
	}
}

func TestRunService_NonPositiveStaleWindowDisablesRecovery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		after time.Duration
	}{
		{name: "zero", after: 0},
		{name: "negative", after: -time.Minute},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)
			registry := newMemoryTurnRequestRegistry()
			registry.seed(run.TurnRequestRecord{
				RunID:     "run-disable-" + tc.name,
				RequestID: "req-disable-" + tc.name,
				TurnID:    "turn-disable-" + tc.name,
				Status:    run.TurnRequestRunning,
				StartedAt: now.Add(-24 * time.Hour),
				UpdatedAt: now.Add(-24 * time.Hour),
			})
			runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "should-not-run"}}
			svc := NewRunServiceWithRegistryConfig(runner, registry, func() time.Time { return now }, RunServiceConfig{
				TurnRequestStaleAfter: tc.after,
			})

			out, err := svc.Run(context.Background(), run.TurnInput{
				RunID:     "run-disable-" + tc.name,
				TurnID:    "turn-disable-" + tc.name,
				RequestID: "req-disable-" + tc.name,
			})
			if err == nil {
				t.Fatal("Run() succeeded, want conflict")
			}
			var verr *v2errors.V2Error
			if !errors.As(err, &verr) {
				t.Fatalf("error type=%T want *V2Error", err)
			}
			if verr.Kind != v2errors.ErrConflict {
				t.Fatalf("kind=%q want %q", verr.Kind, v2errors.ErrConflict)
			}
			if out.TurnID != "turn-disable-"+tc.name {
				t.Fatalf("turn_id=%q want turn-disable-%s", out.TurnID, tc.name)
			}
			if runner.calls != 0 {
				t.Fatalf("runner calls=%d want 0", runner.calls)
			}
		})
	}
}

func TestRunService_StaleRunningRequestWithoutRecovererStillConflicts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)
	memory := newMemoryTurnRequestRegistry()
	memory.seed(run.TurnRequestRecord{
		RunID:     "run-stale-no-recover",
		RequestID: "req-stale-no-recover",
		TurnID:    "turn-stale-no-recover",
		Status:    run.TurnRequestRunning,
		StartedAt: now.Add(-2 * time.Minute),
		UpdatedAt: now.Add(-2 * time.Minute),
	})
	runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "should-not-run"}}
	svc := NewRunServiceWithRegistryConfig(runner, noStaleRecoveryRegistry{registry: memory}, func() time.Time { return now }, RunServiceConfig{
		TurnRequestStaleAfter: time.Minute,
	})

	out, err := svc.Run(context.Background(), run.TurnInput{
		RunID:     "run-stale-no-recover",
		TurnID:    "turn-stale-no-recover",
		RequestID: "req-stale-no-recover",
	})
	if err == nil {
		t.Fatal("Run() succeeded, want conflict")
	}
	var verr *v2errors.V2Error
	if !errors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrConflict {
		t.Fatalf("kind=%q want %q", verr.Kind, v2errors.ErrConflict)
	}
	if out.TurnID != "turn-stale-no-recover" {
		t.Fatalf("turn_id=%q want turn-stale-no-recover", out.TurnID)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls=%d want 0", runner.calls)
	}
}

func TestRunService_StaleRecoveryReturnsCanonicalTerminalResultWhenOriginalCompletesFirst(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)
	registry := newMemoryTurnRequestRegistry()
	registry.seed(run.TurnRequestRecord{
		RunID:     "run-race",
		RequestID: "req-race",
		TurnID:    "turn-race",
		Status:    run.TurnRequestRunning,
		StartedAt: now.Add(-2 * time.Minute),
		UpdatedAt: now.Add(-2 * time.Minute),
	})
	runner := &recordingTurnRunner{
		out: run.TurnOutput{TurnID: "turn-race", Summary: "retry"},
		beforeReturn: func() {
			_, err := registry.CompleteTurnRequest(context.Background(), successfulTurnRequest(
				run.TurnInput{RunID: "run-race", RequestID: "req-race", TurnID: "turn-race"},
				run.TurnOutput{TurnID: "turn-race", Summary: "original"},
				now,
			))
			if err != nil {
				t.Errorf("CompleteTurnRequest() original completion error = %v", err)
			}
		},
	}
	svc := NewRunServiceWithRegistryConfig(runner, registry, func() time.Time { return now }, RunServiceConfig{
		TurnRequestStaleAfter: time.Minute,
	})

	out, err := svc.Run(context.Background(), run.TurnInput{
		RunID:     "run-race",
		TurnID:    "turn-race",
		RequestID: "req-race",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Summary != "original" {
		t.Fatalf("summary=%q want canonical original result", out.Summary)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls=%d want 1", runner.calls)
	}
	rec, err := registry.GetTurnRequest(context.Background(), "run-race", "req-race")
	if err != nil {
		t.Fatalf("GetTurnRequest() error = %v", err)
	}
	if string(rec.OutputJSON) == "" || out.Summary == "retry" {
		t.Fatalf("registry/output did not preserve canonical result: rec=%+v out=%+v", rec, out)
	}
}

func TestRunService_TouchesRunningRequestWhileRunnerIsActive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)
	registry := newMemoryTurnRequestRegistry()
	registry.touchCh = make(chan run.TurnRequestRecord, 1)
	releaseRunner := make(chan struct{})
	runner := &heartbeatBlockingTurnRunner{
		out:     run.TurnOutput{TurnID: "turn-heartbeat", Summary: "completed"},
		release: releaseRunner,
	}
	svc := NewRunServiceWithRegistryConfig(runner, registry, func() time.Time { return now }, RunServiceConfig{
		TurnRequestStaleAfter: 30 * time.Millisecond,
	})

	type runResult struct {
		out run.TurnOutput
		err error
	}
	done := make(chan runResult, 1)
	go func() {
		out, err := svc.Run(context.Background(), run.TurnInput{
			RunID:     "run-heartbeat",
			TurnID:    "turn-heartbeat",
			RequestID: "req-heartbeat",
		})
		done <- runResult{out: out, err: err}
	}()

	var touched run.TurnRequestRecord
	select {
	case touched = <-registry.touchCh:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for turn request heartbeat touch")
	}
	if touched.RunID != "run-heartbeat" || touched.RequestID != "req-heartbeat" || touched.TurnID != "turn-heartbeat" {
		t.Fatalf("touched identity=%+v", touched)
	}
	if touched.Status != run.TurnRequestRunning {
		t.Fatalf("touched status=%q want running", touched.Status)
	}
	close(releaseRunner)

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("Run() error = %v", result.err)
		}
		if result.out.Summary != "completed" {
			t.Fatalf("summary=%q want completed", result.out.Summary)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Run() to finish")
	}
}

func TestRunService_DuplicateSucceededRequestReturnsStoredOutput(t *testing.T) {
	t.Parallel()

	registry := newMemoryTurnRequestRegistry()
	registry.seed(successfulTurnRequest(
		run.TurnInput{RunID: "run-done", RequestID: "req-done", TurnID: "turn-done"},
		run.TurnOutput{TurnID: "turn-done", Summary: "stored", Iterations: 2},
		time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC),
	))
	runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "should-not-run"}}
	svc := NewRunServiceWithRegistry(runner, registry, fixedRunServiceNow())

	out, err := svc.Run(context.Background(), run.TurnInput{
		RunID:     "run-done",
		TurnID:    "turn-done",
		RequestID: "req-done",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Summary != "stored" || out.Iterations != 2 {
		t.Fatalf("output=%+v want stored output", out)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls=%d want 0", runner.calls)
	}
}

func TestRunService_DuplicateFailedRequestReturnsStoredError(t *testing.T) {
	t.Parallel()

	registry := newMemoryTurnRequestRegistry()
	storedErr := &v2errors.V2Error{
		Kind:    v2errors.ErrToolFailed,
		Message: "tool failed once",
		Fatal:   true,
	}
	registry.seed(failedTurnRequest(
		run.TurnInput{RunID: "run-failed", RequestID: "req-failed", TurnID: "turn-failed"},
		run.TurnOutput{TurnID: "turn-failed"},
		storedErr,
		time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC),
	))
	runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "should-not-run"}}
	svc := NewRunServiceWithRegistry(runner, registry, fixedRunServiceNow())

	_, err := svc.Run(context.Background(), run.TurnInput{
		RunID:     "run-failed",
		TurnID:    "turn-failed",
		RequestID: "req-failed",
	})
	if err == nil {
		t.Fatal("Run() succeeded, want stored error")
	}
	var verr *v2errors.V2Error
	if !errors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrToolFailed || verr.Message != "tool failed once" {
		t.Fatalf("stored error=%+v", verr)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls=%d want 0", runner.calls)
	}
}

func TestRunService_DuplicateCanceledRequestReturnsStoredError(t *testing.T) {
	t.Parallel()

	registry := newMemoryTurnRequestRegistry()
	storedErr := &v2errors.V2Error{
		Kind:      v2errors.ErrTimeout,
		Message:   "request canceled once",
		Fatal:     true,
		Retryable: true,
	}
	registry.seed(failedTurnRequest(
		run.TurnInput{RunID: "run-canceled", RequestID: "req-canceled", TurnID: "turn-canceled"},
		run.TurnOutput{TurnID: "turn-canceled"},
		storedErr,
		time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC),
	))
	runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "should-not-run"}}
	svc := NewRunServiceWithRegistry(runner, registry, fixedRunServiceNow())

	_, err := svc.Run(context.Background(), run.TurnInput{
		RunID:     "run-canceled",
		TurnID:    "turn-canceled",
		RequestID: "req-canceled",
	})
	if err == nil {
		t.Fatal("Run() succeeded, want stored error")
	}
	var verr *v2errors.V2Error
	if !errors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != v2errors.ErrTimeout || verr.Message != "request canceled once" {
		t.Fatalf("stored error=%+v", verr)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls=%d want 0", runner.calls)
	}
}

func TestRunService_FirstRequestCompletesRegistry(t *testing.T) {
	t.Parallel()

	registry := newMemoryTurnRequestRegistry()
	runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "turn-new", Summary: "completed"}}
	svc := NewRunServiceWithRegistry(runner, registry, fixedRunServiceNow())

	out, err := svc.Run(context.Background(), run.TurnInput{
		RunID:     "run-new",
		TurnID:    "turn-new",
		RequestID: "req-new",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Summary != "completed" {
		t.Fatalf("summary=%q want completed", out.Summary)
	}
	rec, err := registry.GetTurnRequest(context.Background(), "run-new", "req-new")
	if err != nil {
		t.Fatalf("GetTurnRequest() error = %v", err)
	}
	if rec.Status != run.TurnRequestSucceeded {
		t.Fatalf("status=%q want %q", rec.Status, run.TurnRequestSucceeded)
	}
	if len(rec.OutputJSON) == 0 {
		t.Fatal("output_json is empty")
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls=%d want 1", runner.calls)
	}
}

func TestRunService_FailedRequestCompletesRegistryWithError(t *testing.T) {
	t.Parallel()

	registry := newMemoryTurnRequestRegistry()
	runnerErr := &v2errors.V2Error{
		Kind:    v2errors.ErrStageFailed,
		Message: "stage failed",
		Fatal:   true,
	}
	runner := &recordingTurnRunner{out: run.TurnOutput{TurnID: "turn-error"}, err: runnerErr}
	svc := NewRunServiceWithRegistry(runner, registry, fixedRunServiceNow())

	_, err := svc.Run(context.Background(), run.TurnInput{
		RunID:     "run-error",
		TurnID:    "turn-error",
		RequestID: "req-error",
	})
	if err == nil {
		t.Fatal("Run() succeeded, want runner error")
	}
	rec, getErr := registry.GetTurnRequest(context.Background(), "run-error", "req-error")
	if getErr != nil {
		t.Fatalf("GetTurnRequest() error = %v", getErr)
	}
	if rec.Status != run.TurnRequestFailed {
		t.Fatalf("status=%q want %q", rec.Status, run.TurnRequestFailed)
	}
	if len(rec.ErrorJSON) == 0 {
		t.Fatal("error_json is empty")
	}
}

type recordingTurnRunner struct {
	out          run.TurnOutput
	err          error
	beforeReturn func()
	calls        int
	lastIn       run.TurnInput
}

func (r *recordingTurnRunner) RunTurn(_ context.Context, in run.TurnInput) (run.TurnOutput, error) {
	r.calls++
	r.lastIn = in
	if r.beforeReturn != nil {
		r.beforeReturn()
	}
	return r.out, r.err
}

type heartbeatBlockingTurnRunner struct {
	out     run.TurnOutput
	err     error
	release <-chan struct{}
}

func (r *heartbeatBlockingTurnRunner) RunTurn(ctx context.Context, _ run.TurnInput) (run.TurnOutput, error) {
	select {
	case <-r.release:
		return r.out, r.err
	case <-ctx.Done():
		return run.TurnOutput{}, ctx.Err()
	}
}

type memoryTurnRequestRegistry struct {
	mu      sync.Mutex
	records map[string]run.TurnRequestRecord
	touchCh chan run.TurnRequestRecord
}

func newMemoryTurnRequestRegistry() *memoryTurnRequestRegistry {
	return &memoryTurnRequestRegistry{records: map[string]run.TurnRequestRecord{}}
}

func (r *memoryTurnRequestRegistry) seed(rec run.TurnRequestRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[turnRequestKey(rec.RunID, rec.RequestID)] = rec.Clone()
}

func (r *memoryTurnRequestRegistry) BeginTurnRequest(_ context.Context, rec run.TurnRequestRecord) (run.TurnRequestRecord, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := turnRequestKey(rec.RunID, rec.RequestID)
	existing, ok := r.records[key]
	if ok {
		if existing.TurnID != rec.TurnID {
			return existing.Clone(), false, run.ErrTurnRequestConflict
		}
		return existing.Clone(), false, nil
	}
	r.records[key] = rec.Clone()
	return rec.Clone(), true, nil
}

func (r *memoryTurnRequestRegistry) RecoverStaleTurnRequest(_ context.Context, rec run.TurnRequestRecord, staleBefore time.Time) (run.TurnRequestRecord, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := turnRequestKey(rec.RunID, rec.RequestID)
	existing, ok := r.records[key]
	if !ok {
		return run.TurnRequestRecord{}, false, run.ErrTurnRequestNotFound
	}
	if existing.TurnID != rec.TurnID {
		return existing.Clone(), false, run.ErrTurnRequestConflict
	}
	if existing.Status != run.TurnRequestRunning {
		return existing.Clone(), false, nil
	}
	lastActivity := turnRequestLastActivity(existing)
	if lastActivity.IsZero() || !lastActivity.Before(staleBefore.UTC()) {
		return existing.Clone(), false, nil
	}
	rec.Status = run.TurnRequestRunning
	rec.OutputJSON = nil
	rec.ErrorJSON = nil
	rec.CompletedAt = time.Time{}
	r.records[key] = rec.Clone()
	return rec.Clone(), true, nil
}

func (r *memoryTurnRequestRegistry) TouchTurnRequest(_ context.Context, runID, requestID, turnID string, now time.Time) (run.TurnRequestRecord, bool, error) {
	r.mu.Lock()
	key := turnRequestKey(runID, requestID)
	existing, ok := r.records[key]
	if !ok {
		r.mu.Unlock()
		return run.TurnRequestRecord{}, false, run.ErrTurnRequestNotFound
	}
	if existing.TurnID != turnID {
		out := existing.Clone()
		r.mu.Unlock()
		return out, false, run.ErrTurnRequestConflict
	}
	if existing.Status != run.TurnRequestRunning {
		out := existing.Clone()
		r.mu.Unlock()
		return out, false, nil
	}
	existing.UpdatedAt = now.UTC()
	r.records[key] = existing.Clone()
	out := existing.Clone()
	touchCh := r.touchCh
	r.mu.Unlock()

	if touchCh != nil {
		select {
		case touchCh <- out.Clone():
		default:
		}
	}
	return out, true, nil
}

func (r *memoryTurnRequestRegistry) CompleteTurnRequest(_ context.Context, rec run.TurnRequestRecord) (run.TurnRequestRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := turnRequestKey(rec.RunID, rec.RequestID)
	existing, ok := r.records[key]
	if !ok {
		return run.TurnRequestRecord{}, run.ErrTurnRequestNotFound
	}
	if existing.Status.IsTerminal() {
		return existing.Clone(), nil
	}
	rec.StartedAt = existing.StartedAt
	r.records[key] = rec.Clone()
	return rec.Clone(), nil
}

func (r *memoryTurnRequestRegistry) GetTurnRequest(_ context.Context, runID, requestID string) (run.TurnRequestRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[turnRequestKey(runID, requestID)]
	if !ok {
		return run.TurnRequestRecord{}, run.ErrTurnRequestNotFound
	}
	return rec.Clone(), nil
}

type noStaleRecoveryRegistry struct {
	registry *memoryTurnRequestRegistry
}

func (r noStaleRecoveryRegistry) BeginTurnRequest(ctx context.Context, rec run.TurnRequestRecord) (run.TurnRequestRecord, bool, error) {
	return r.registry.BeginTurnRequest(ctx, rec)
}

func (r noStaleRecoveryRegistry) CompleteTurnRequest(ctx context.Context, rec run.TurnRequestRecord) (run.TurnRequestRecord, error) {
	return r.registry.CompleteTurnRequest(ctx, rec)
}

func (r noStaleRecoveryRegistry) GetTurnRequest(ctx context.Context, runID, requestID string) (run.TurnRequestRecord, error) {
	return r.registry.GetTurnRequest(ctx, runID, requestID)
}

func turnRequestKey(runID, requestID string) string {
	return runID + "\x00" + requestID
}

func fixedRunServiceNow() func() time.Time {
	return func() time.Time {
		return time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)
	}
}
