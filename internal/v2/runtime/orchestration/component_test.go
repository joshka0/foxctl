package orchestration

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestComponent_RunImmediateCycle(t *testing.T) {
	t.Parallel()

	scheduler := &fakeTickRunner{}
	recovery := &fakeStartupRecovery{}
	reconciler := &fakeReconciler{}

	component := NewComponent(ComponentConfig{
		PollInterval: time.Hour,
		Scheduler:    scheduler,
		Recovery:     recovery,
		Reconciler:   reconciler,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- component.Run(ctx)
	}()

	waitForCount(t, &scheduler.calls, 1, 2*time.Second, "scheduler tick")
	waitForCount(t, &recovery.calls, 1, 2*time.Second, "startup recovery")
	waitForCount(t, &reconciler.calls, 1, 2*time.Second, "reconcile run")
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component stop")
	}
}

func TestComponent_RecoveryRunsBeforeReconcileAndScheduler(t *testing.T) {
	t.Parallel()

	steps := make(chan string, 3)
	component := NewComponent(ComponentConfig{
		PollInterval: time.Hour,
		Scheduler: TickRunnerFunc(func(context.Context) error {
			steps <- "scheduler"
			return nil
		}),
		Recovery: StartupRecoveryFunc(func(context.Context) error {
			steps <- "recovery"
			return nil
		}),
		Reconciler: ReconcileFunc(func(context.Context) error {
			steps <- "reconciler"
			return nil
		}),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- component.Run(ctx)
	}()

	got := []string{
		waitForStep(t, steps, "first step"),
		waitForStep(t, steps, "second step"),
		waitForStep(t, steps, "third step"),
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component stop")
	}

	want := []string{"recovery", "reconciler", "scheduler"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("steps=%v want %v", got, want)
		}
	}
}

func TestComponent_RecoveryFailureSkipsReconcilePreflightAndScheduler(t *testing.T) {
	t.Parallel()

	scheduler := &fakeTickRunner{}
	recoveryErr := errors.New("runtime unavailable")
	recovery := &fakeStartupRecovery{err: recoveryErr}
	reconciler := &fakeReconciler{}
	preflight := atomic.Int64{}
	errCh := make(chan error, 1)

	component := NewComponent(ComponentConfig{
		PollInterval: time.Hour,
		Scheduler:    scheduler,
		Recovery:     recovery,
		Reconciler:   reconciler,
		Preflight: PreflightFunc(func(context.Context) error {
			preflight.Add(1)
			return nil
		}),
		OnError: func(err error) {
			errCh <- err
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- component.Run(ctx)
	}()

	waitForCount(t, &recovery.calls, 1, 2*time.Second, "startup recovery")
	select {
	case err := <-errCh:
		if !errors.Is(err, recoveryErr) {
			t.Fatalf("onError err=%v want %v", err, recoveryErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for recovery error")
	}

	if got := reconciler.calls.Load(); got != 0 {
		t.Fatalf("reconciler calls=%d want 0", got)
	}
	if got := preflight.Load(); got != 0 {
		t.Fatalf("preflight calls=%d want 0", got)
	}
	if got := scheduler.calls.Load(); got != 0 {
		t.Fatalf("scheduler calls=%d want 0", got)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component stop")
	}
}

func TestComponent_PreflightFailureSkipsSchedulerButRunsReconcile(t *testing.T) {
	t.Parallel()

	scheduler := &fakeTickRunner{}
	reconciler := &fakeReconciler{}
	preflightErr := errors.New("bad config")
	errCh := make(chan error, 2)

	component := NewComponent(ComponentConfig{
		PollInterval: time.Hour,
		Scheduler:    scheduler,
		Reconciler:   reconciler,
		Preflight: PreflightFunc(func(context.Context) error {
			return preflightErr
		}),
		OnError: func(err error) {
			errCh <- err
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- component.Run(ctx)
	}()

	waitForCount(t, &reconciler.calls, 1, 2*time.Second, "reconcile run")
	select {
	case err := <-errCh:
		if !errors.Is(err, preflightErr) {
			t.Fatalf("onError err=%v want %v", err, preflightErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for preflight error")
	}

	if got := scheduler.calls.Load(); got != 0 {
		t.Fatalf("scheduler calls=%d want 0", got)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component stop")
	}
}

func TestComponent_MissingSchedulerReturnsError(t *testing.T) {
	t.Parallel()

	component := NewComponent(ComponentConfig{})
	err := component.Run(context.Background())
	if !errors.Is(err, ErrMissingTickRunner) {
		t.Fatalf("Run() error=%v want %v", err, ErrMissingTickRunner)
	}
}

func TestComponent_RunCanceledContextDoesNotExecuteCycle(t *testing.T) {
	t.Parallel()

	scheduler := &fakeTickRunner{}
	reconciler := &fakeReconciler{}
	component := NewComponent(ComponentConfig{
		PollInterval: time.Hour,
		Scheduler:    scheduler,
		Reconciler:   reconciler,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := component.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := scheduler.calls.Load(); got != 0 {
		t.Fatalf("scheduler calls=%d want 0", got)
	}
	if got := reconciler.calls.Load(); got != 0 {
		t.Fatalf("reconciler calls=%d want 0", got)
	}
}

type fakeTickRunner struct {
	calls atomic.Int64
	err   error
}

func (f *fakeTickRunner) Tick(context.Context) error {
	f.calls.Add(1)
	return f.err
}

type TickRunnerFunc func(ctx context.Context) error

func (f TickRunnerFunc) Tick(ctx context.Context) error {
	if f == nil {
		return nil
	}
	return f(ctx)
}

type fakeReconciler struct {
	calls atomic.Int64
	err   error
}

func (f *fakeReconciler) Reconcile(context.Context) error {
	f.calls.Add(1)
	return f.err
}

type fakeStartupRecovery struct {
	calls atomic.Int64
	err   error
}

func (f *fakeStartupRecovery) RecoverOrphanedRuns(context.Context) error {
	f.calls.Add(1)
	return f.err
}

func waitForCount(t *testing.T, counter *atomic.Int64, want int64, timeout time.Duration, label string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if counter.Load() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s count >= %d (got %d)", label, want, counter.Load())
}

func waitForStep(t *testing.T, steps <-chan string, label string) string {
	t.Helper()
	select {
	case step := <-steps:
		return step
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for %s", label)
		return ""
	}
}
