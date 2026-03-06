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
	reconciler := &fakeReconciler{}

	component := NewComponent(ComponentConfig{
		PollInterval: time.Hour,
		Scheduler:    scheduler,
		Reconciler:   reconciler,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- component.Run(ctx)
	}()

	waitForCount(t, &scheduler.calls, 1, 2*time.Second, "scheduler tick")
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

type fakeReconciler struct {
	calls atomic.Int64
	err   error
}

func (f *fakeReconciler) Reconcile(context.Context) error {
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
