package supervisor_test

import (
	"context"
	stderrors "errors"
	"sync"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/v2/runtime/supervisor"
)

func TestSupervisor_StartsAndStopsAllComponents(t *testing.T) {
	t.Parallel()

	c1 := newBlockingComponent(nil)
	c2 := newBlockingComponent(nil)

	host := supervisor.NewHost([]supervisor.Spec{
		{Name: "component-1", Component: c1},
		{Name: "component-2", Component: c2},
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- host.Run(ctx) }()

	waitStarted(t, c1.started)
	waitStarted(t, c2.started)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for host shutdown")
	}

	waitStopped(t, c1.stopped)
	waitStopped(t, c2.stopped)
}

func TestSupervisor_ContextCancel_StopsComponents(t *testing.T) {
	t.Parallel()

	c1 := newBlockingComponent(nil)
	host := supervisor.NewHost([]supervisor.Spec{
		{Name: "component-1", Component: c1},
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- host.Run(ctx) }()

	waitStarted(t, c1.started)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for host cancellation")
	}

	waitStopped(t, c1.stopped)
}

func TestSupervisor_ComponentError_FailsHost(t *testing.T) {
	t.Parallel()

	expected := stderrors.New("boom")
	bad := newBlockingComponent(expected)
	other := newBlockingComponent(nil)

	host := supervisor.NewHost([]supervisor.Spec{
		{Name: "bad-component", Component: bad},
		{Name: "other-component", Component: other},
	}, nil)

	done := make(chan error, 1)
	go func() { done <- host.Run(context.Background()) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected host error")
		}
		if !stderrors.Is(err, expected) {
			t.Fatalf("error=%v want wrapped %v", err, expected)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for host failure")
	}

	waitStopped(t, other.stopped)
}

type blockingComponent struct {
	runErr  error
	started chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func newBlockingComponent(runErr error) *blockingComponent {
	return &blockingComponent{
		runErr:  runErr,
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (c *blockingComponent) Run(ctx context.Context) error {
	c.once.Do(func() { close(c.started) })
	if c.runErr != nil {
		close(c.stopped)
		return c.runErr
	}
	<-ctx.Done()
	close(c.stopped)
	return nil
}

func waitStarted(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component start")
	}
}

func waitStopped(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for component stop")
	}
}
