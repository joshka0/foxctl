package services

import (
	"context"
	stderrors "errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/runtime/supervisor"
)

func TestBuildLongLivedRunSpecs_DeterministicOrderAndNilSkip(t *testing.T) {
	t.Parallel()

	specs := BuildLongLivedRunSpecs(LongLivedRunComponents{
		EnricherProducer:  &testComponent{started: make(chan struct{}), stopped: make(chan struct{})},
		NarrativeCompiler: &testComponent{started: make(chan struct{}), stopped: make(chan struct{})},
	})
	if len(specs) != 2 {
		t.Fatalf("specs len=%d want 2", len(specs))
	}
	if specs[0].Name != componentNameEnricherProducer {
		t.Fatalf("spec[0].name=%q want %q", specs[0].Name, componentNameEnricherProducer)
	}
	if specs[1].Name != componentNameNarrativeCompiler {
		t.Fatalf("spec[1].name=%q want %q", specs[1].Name, componentNameNarrativeCompiler)
	}
}

func TestBuildLongLivedRunSpecs_IncludesOrchestrationComponent(t *testing.T) {
	t.Parallel()

	orchestrationComponent := &testComponent{started: make(chan struct{}), stopped: make(chan struct{})}
	specs := BuildLongLivedRunSpecs(LongLivedRunComponents{
		Orchestration: orchestrationComponent,
	})
	if len(specs) != 1 {
		t.Fatalf("specs len=%d want 1", len(specs))
	}
	if specs[0].Name != componentNameOrchestration {
		t.Fatalf("spec[0].name=%q want %q", specs[0].Name, componentNameOrchestration)
	}
}

func TestLongLivedRunService_RunStartsHostOnce(t *testing.T) {
	t.Parallel()

	component := &testComponent{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
	runner := &testTurnRunner{
		out: run.TurnOutput{TurnID: "turn-1"},
	}

	svc := NewLongLivedRunService(
		runner,
		BuildLongLivedRunSpecs(LongLivedRunComponents{
			EnricherProducer: component,
		}),
		nil,
	)

	_, err := svc.Run(context.Background(), run.TurnInput{
		RunID:   "run-1",
		Prompt:  "first",
		Command: "run",
	})
	if err != nil {
		t.Fatalf("Run(first) error = %v", err)
	}

	_, err = svc.Run(context.Background(), run.TurnInput{
		RunID:   "run-1",
		Prompt:  "second",
		Command: "run",
	})
	if err != nil {
		t.Fatalf("Run(second) error = %v", err)
	}
	if runner.calls.Load() != 2 {
		t.Fatalf("runner calls=%d want 2", runner.calls.Load())
	}

	waitSignal(t, component.started, "component start")

	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := svc.Close(closeCtx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	waitSignal(t, component.stopped, "component stop")
}

func TestLongLivedRunService_RunReturnsDependencyErrorWhenHostFails(t *testing.T) {
	t.Parallel()

	component := &testComponent{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
		runErr:  stderrors.New("component boom"),
	}
	runner := &testTurnRunner{
		out: run.TurnOutput{TurnID: "turn-1"},
	}

	svc := NewLongLivedRunService(
		runner,
		BuildLongLivedRunSpecs(LongLivedRunComponents{
			EnricherProducer: component,
		}),
		nil,
	)

	// First run may succeed while host failure is still propagating.
	_, _ = svc.Run(context.Background(), run.TurnInput{
		RunID:   "run-host-failure",
		Prompt:  "first",
		Command: "run",
	})

	gotErr := waitForError(5*time.Second, func() error {
		_, err := svc.Run(context.Background(), run.TurnInput{
			RunID:   "run-host-failure",
			Prompt:  "retry",
			Command: "run",
		})
		return err
	})
	if gotErr == nil {
		t.Fatal("expected host failure to surface on subsequent Run")
	}
	var verr *errors.V2Error
	if !stderrors.As(gotErr, &verr) {
		t.Fatalf("error type=%T want *V2Error", gotErr)
	}
	if verr.Kind != errors.ErrDependency {
		t.Fatalf("error kind=%q want %q", verr.Kind, errors.ErrDependency)
	}
	if !verr.Fatal {
		t.Fatalf("error fatal=%v want true", verr.Fatal)
	}
	if verr.Retryable {
		t.Fatalf("error retryable=%v want false", verr.Retryable)
	}
}

func TestLongLivedRunService_NoHostStillRuns(t *testing.T) {
	t.Parallel()

	runner := &testTurnRunner{
		out: run.TurnOutput{TurnID: "turn-no-host"},
	}
	svc := NewLongLivedRunService(runner, nil, nil)

	out, err := svc.Run(context.Background(), run.TurnInput{
		RunID:   "run-no-host",
		Prompt:  "hello",
		Command: "run",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.TurnID != "turn-no-host" {
		t.Fatalf("turn_id=%q want turn-no-host", out.TurnID)
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := svc.Close(closeCtx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err = svc.Run(context.Background(), run.TurnInput{
		RunID:   "run-no-host",
		Prompt:  "after-close",
		Command: "run",
	})
	if err == nil {
		t.Fatal("expected Run after Close to fail for no-host service")
	}
}

func TestLongLivedRunService_RunAfterCloseReturnsDependencyError(t *testing.T) {
	t.Parallel()

	component := &testComponent{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
	runner := &testTurnRunner{
		out: run.TurnOutput{TurnID: "turn-closed"},
	}
	svc := NewLongLivedRunService(
		runner,
		BuildLongLivedRunSpecs(LongLivedRunComponents{
			EnricherProducer: component,
		}),
		nil,
	)

	_, err := svc.Run(context.Background(), run.TurnInput{
		RunID:   "run-close-check",
		Prompt:  "start",
		Command: "run",
	})
	if err != nil {
		t.Fatalf("Run(start) error = %v", err)
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := svc.Close(closeCtx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err = svc.Run(context.Background(), run.TurnInput{
		RunID:   "run-close-check",
		Prompt:  "after-close",
		Command: "run",
	})
	if err == nil {
		t.Fatal("expected Run after Close to fail")
	}
	var verr *errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("error type=%T want *V2Error", err)
	}
	if verr.Kind != errors.ErrDependency {
		t.Fatalf("error kind=%q want %q", verr.Kind, errors.ErrDependency)
	}
	if !verr.Fatal {
		t.Fatalf("error fatal=%v want true", verr.Fatal)
	}
	if verr.Retryable {
		t.Fatalf("error retryable=%v want false", verr.Retryable)
	}
}

func TestLongLivedRunService_CloseTimeoutCanBeRetried(t *testing.T) {
	t.Parallel()

	component := &stubbornComponent{
		started:   make(chan struct{}),
		stopped:   make(chan struct{}),
		allowStop: make(chan struct{}),
	}
	runner := &testTurnRunner{
		out: run.TurnOutput{TurnID: "turn-timeout-close"},
	}
	svc := NewLongLivedRunService(
		runner,
		BuildLongLivedRunSpecs(LongLivedRunComponents{
			EnricherProducer: component,
		}),
		nil,
	)

	_, err := svc.Run(context.Background(), run.TurnInput{
		RunID:   "run-timeout-close",
		Prompt:  "start",
		Command: "run",
	})
	if err != nil {
		t.Fatalf("Run(start) error = %v", err)
	}
	waitSignal(t, component.started, "stubborn component start")

	timeoutCtx, cancelTimeout := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelTimeout()
	if err := svc.Close(timeoutCtx); !stderrors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close(timeout) error = %v, want deadline exceeded", err)
	}

	close(component.allowStop)

	retryCtx, cancelRetry := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelRetry()
	if err := svc.Close(retryCtx); err != nil {
		t.Fatalf("Close(retry) error = %v", err)
	}
	waitSignal(t, component.stopped, "stubborn component stop")
}

func TestLongLivedRunService_AllInvalidSpecs_NoHostCreated(t *testing.T) {
	t.Parallel()

	invalidComponent := &testComponent{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
	runner := &testTurnRunner{
		out: run.TurnOutput{TurnID: "turn-invalid-specs"},
	}
	svc := NewLongLivedRunService(
		runner,
		[]supervisor.Spec{
			{Name: "   ", Component: invalidComponent},
			{Name: "valid", Component: nil},
		},
		nil,
	)
	if svc.host != nil {
		t.Fatal("expected host to be nil when all specs are invalid")
	}

	for i := 0; i < 2; i++ {
		if _, err := svc.Run(context.Background(), run.TurnInput{
			RunID:   "run-invalid-specs",
			Prompt:  "hello",
			Command: "run",
		}); err != nil {
			t.Fatalf("Run(%d) error = %v", i, err)
		}
	}
	if runner.calls.Load() != 2 {
		t.Fatalf("runner calls=%d want 2", runner.calls.Load())
	}
	select {
	case <-invalidComponent.started:
		t.Fatal("invalid component unexpectedly started")
	case <-time.After(120 * time.Millisecond):
	}
}

func TestLongLivedRunService_CloseWaitsForInFlightRun(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	runner := &blockingTurnRunner{
		started: make(chan struct{}),
		block:   block,
		out:     run.TurnOutput{TurnID: "turn-blocking"},
	}
	svc := NewLongLivedRunService(runner, nil, nil)

	runDone := make(chan error, 1)
	go func() {
		_, err := svc.Run(context.Background(), run.TurnInput{
			RunID:   "run-blocking",
			Prompt:  "first",
			Command: "run",
		})
		runDone <- err
	}()

	waitSignal(t, runner.started, "blocking run start")

	closeDone := make(chan error, 1)
	go func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		closeDone <- svc.Close(closeCtx)
	}()

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before in-flight run completed: %v", err)
	case <-time.After(120 * time.Millisecond):
	}

	close(block)

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run(blocking) error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for in-flight Run to complete")
	}

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Close to complete")
	}

	_, err := svc.Run(context.Background(), run.TurnInput{
		RunID:   "run-after-close",
		Prompt:  "second",
		Command: "run",
	})
	if err == nil {
		t.Fatal("expected Run after Close to fail")
	}
}

type testTurnRunner struct {
	out   run.TurnOutput
	err   error
	calls atomic.Int32
}

func (r *testTurnRunner) RunTurn(_ context.Context, _ run.TurnInput) (run.TurnOutput, error) {
	r.calls.Add(1)
	return r.out, r.err
}

type blockingTurnRunner struct {
	started chan struct{}
	block   <-chan struct{}
	out     run.TurnOutput
	err     error
}

func (r *blockingTurnRunner) RunTurn(_ context.Context, _ run.TurnInput) (run.TurnOutput, error) {
	if r.started != nil {
		select {
		case <-r.started:
		default:
			close(r.started)
		}
	}
	if r.block != nil {
		<-r.block
	}
	return r.out, r.err
}

type testComponent struct {
	started chan struct{}
	stopped chan struct{}
	runErr  error
}

func (c *testComponent) Run(ctx context.Context) error {
	if c.started != nil {
		select {
		case <-c.started:
		default:
			close(c.started)
		}
	}
	defer func() {
		if c.stopped != nil {
			select {
			case <-c.stopped:
			default:
				close(c.stopped)
			}
		}
	}()
	if c.runErr != nil {
		return c.runErr
	}
	<-ctx.Done()
	return nil
}

type stubbornComponent struct {
	started   chan struct{}
	stopped   chan struct{}
	allowStop chan struct{}
}

func (c *stubbornComponent) Run(ctx context.Context) error {
	if c.started != nil {
		select {
		case <-c.started:
		default:
			close(c.started)
		}
	}
	<-ctx.Done()
	<-c.allowStop
	if c.stopped != nil {
		select {
		case <-c.stopped:
		default:
			close(c.stopped)
		}
	}
	return nil
}

func waitSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for %s", label)
	}
}

func waitForError(timeout time.Duration, runFn func() error) error {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	backoff := 10 * time.Millisecond
	for {
		err := runFn()
		if err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(backoff)
		if backoff < 100*time.Millisecond {
			backoff *= 2
			if backoff > 100*time.Millisecond {
				backoff = 100 * time.Millisecond
			}
		}
	}
}
