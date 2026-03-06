package orchestration

import (
	"context"
	"errors"
	"time"
)

const defaultPollInterval = 30 * time.Second

// ErrMissingTickRunner indicates scheduler dependency is nil.
var ErrMissingTickRunner = errors.New("v2 orchestration: missing tick runner")

// TickRunner runs one schedule tick.
type TickRunner interface {
	Tick(ctx context.Context) error
}

// PreflightChecker validates dispatch preconditions for this cycle.
type PreflightChecker interface {
	Check(ctx context.Context) error
}

// PreflightFunc adapts a function to PreflightChecker.
type PreflightFunc func(ctx context.Context) error

// Check runs one preflight check.
func (f PreflightFunc) Check(ctx context.Context) error {
	if f == nil {
		return nil
	}
	return f(ctx)
}

// ComponentConfig wires the orchestration component lifecycle.
type ComponentConfig struct {
	PollInterval time.Duration
	Scheduler    TickRunner
	Reconciler   Reconciler
	Preflight    PreflightChecker
	OnError      func(error)
}

// Component is the long-lived orchestration scheduler/reconcile loop.
type Component struct {
	pollInterval time.Duration
	scheduler    TickRunner
	reconciler   Reconciler
	preflight    PreflightChecker
	onError      func(error)
}

// NewComponent creates a runtime component suitable for supervisor hosting.
func NewComponent(cfg ComponentConfig) *Component {
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	onError := cfg.OnError
	if onError == nil {
		onError = func(error) {}
	}

	return &Component{
		pollInterval: pollInterval,
		scheduler:    cfg.Scheduler,
		reconciler:   cfg.Reconciler,
		preflight:    cfg.Preflight,
		onError:      onError,
	}
}

// Run executes periodic reconcile + dispatch cycles until context cancellation.
func (c *Component) Run(ctx context.Context) error {
	if c == nil || c.scheduler == nil {
		return ErrMissingTickRunner
	}

	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	select {
	case <-ctx.Done():
		return nil
	default:
	}

	// Kick off one immediate cycle before waiting for first tick.
	c.runCycle(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.runCycle(ctx)
		}
	}
}

func (c *Component) runCycle(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return
	}

	if c.reconciler != nil {
		if err := c.reconciler.Reconcile(ctx); err != nil {
			c.onError(err)
		}
	}

	if c.preflight != nil {
		if err := c.preflight.Check(ctx); err != nil {
			c.onError(err)
			return
		}
	}

	if err := c.scheduler.Tick(ctx); err != nil {
		c.onError(err)
	}
}
