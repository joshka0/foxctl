package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	"github.com/joshka0/foxctl/internal/v2/core/run"
	"github.com/joshka0/foxctl/internal/v2/runtime/supervisor"
)

const (
	componentNameEnricherProducer  = "enricher.producer"
	componentNameEnricherWorker    = "enricher.worker"
	componentNameEpisodeCompiler   = "enricher.episode_compiler"
	componentNameNarrativeCompiler = "enricher.narrative_compiler"
	componentNameMaintenanceDigest = "maintenance.digest"
	componentNameOrchestration     = "orchestration.component"
)

var (
	errRuntimeComponentHostStopped = errors.New("runtime component host stopped")
	errLongLivedRunServiceClosed   = errors.New("long-lived run service is closed")
)

// LongLivedRunComponents declares optional background components attached to a long-lived run service.
type LongLivedRunComponents struct {
	EnricherProducer  supervisor.Component
	EnricherWorker    supervisor.Component
	EpisodeCompiler   supervisor.Component
	NarrativeCompiler supervisor.Component
	MaintenanceDigest supervisor.Component
	Orchestration     supervisor.Component
}

// BuildLongLivedRunSpecs converts optional components into deterministic supervisor specs.
func BuildLongLivedRunSpecs(components LongLivedRunComponents) []supervisor.Spec {
	specs := make([]supervisor.Spec, 0, 5)
	appendSpec := func(name string, component supervisor.Component) {
		if component == nil {
			return
		}
		specs = append(specs, supervisor.Spec{
			Name:      name,
			Component: component,
		})
	}

	appendSpec(componentNameEnricherProducer, components.EnricherProducer)
	appendSpec(componentNameEnricherWorker, components.EnricherWorker)
	appendSpec(componentNameEpisodeCompiler, components.EpisodeCompiler)
	appendSpec(componentNameNarrativeCompiler, components.NarrativeCompiler)
	appendSpec(componentNameMaintenanceDigest, components.MaintenanceDigest)
	appendSpec(componentNameOrchestration, components.Orchestration)

	return specs
}

// LongLivedRunService runs canonical turns while keeping background components alive via supervisor host.
//
// Turn execution remains request-scoped and synchronous. Background components are started lazily
// on first run and continue under a dedicated host context until Close is called.
type LongLivedRunService struct {
	run  *RunService
	host *supervisor.Host

	runGate sync.RWMutex

	mu         sync.Mutex
	hostCancel context.CancelFunc
	hostDone   chan error
	hostErr    error
	started    bool
	closed     bool
}

// NewLongLivedRunService builds a long-lived run service with optional background components.
func NewLongLivedRunService(
	runner TurnRunner,
	specs []supervisor.Spec,
	observe supervisor.Observer,
) *LongLivedRunService {
	return newLongLivedRunService(NewRunService(runner), specs, observe)
}

func newLongLivedRunService(
	runSvc *RunService,
	specs []supervisor.Spec,
	observe supervisor.Observer,
) *LongLivedRunService {
	filteredSpecs := sanitizeSupervisorSpecs(specs)
	var host *supervisor.Host
	if len(filteredSpecs) > 0 {
		host = supervisor.NewHost(filteredSpecs, observe)
	}
	return &LongLivedRunService{
		run:  runSvc,
		host: host,
	}
}

// Run executes one canonical turn and ensures background host components are active.
func (s *LongLivedRunService) Run(ctx context.Context, in run.TurnInput) (run.TurnOutput, error) {
	if s == nil || s.run == nil {
		return run.TurnOutput{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "run service is not configured",
			Fatal:   true,
		}
	}

	s.runGate.RLock()
	defer s.runGate.RUnlock()

	if err := s.ensureHostStarted(); err != nil {
		retryable, fatal := classifyHostDependencyError(err)
		return run.TurnOutput{}, &v2errors.V2Error{
			Kind:      v2errors.ErrDependency,
			Message:   "runtime component host unavailable",
			Cause:     err,
			Fatal:     fatal,
			Retryable: retryable,
		}
	}
	return s.run.Run(ctx, in)
}

// Close stops background components and waits for host shutdown.
func (s *LongLivedRunService) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}

	s.runGate.Lock()
	defer s.runGate.Unlock()

	s.mu.Lock()
	s.closed = true
	cancel := s.hostCancel
	done := s.hostDone
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case err, ok := <-done:
		s.mu.Lock()
		s.hostCancel = nil
		s.hostDone = nil
		s.mu.Unlock()
		if !ok || err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *LongLivedRunService) ensureHostStarted() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errLongLivedRunServiceClosed
	}
	if s.host == nil {
		return nil
	}
	if err := s.checkHostDoneLocked(); err != nil {
		return err
	}
	if s.started {
		return nil
	}

	hostCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	host := s.host

	s.hostCancel = cancel
	s.hostDone = done
	s.started = true

	go func() {
		done <- host.Run(hostCtx)
		close(done)
	}()
	return nil
}

func (s *LongLivedRunService) checkHostDoneLocked() error {
	if s.hostDone == nil {
		return s.hostErr
	}
	select {
	case err, ok := <-s.hostDone:
		if !ok {
			s.hostDone = nil
			if s.hostErr != nil {
				return s.hostErr
			}
			s.hostErr = errRuntimeComponentHostStopped
			return s.hostErr
		}
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.hostDone = nil
			s.hostErr = errRuntimeComponentHostStopped
			return s.hostErr
		}
		s.hostDone = nil
		s.hostErr = fmt.Errorf("%w: %s", errRuntimeComponentHostStopped, strings.TrimSpace(err.Error()))
		return s.hostErr
	default:
		return nil
	}
}

func sanitizeSupervisorSpecs(specs []supervisor.Spec) []supervisor.Spec {
	out := make([]supervisor.Spec, 0, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" || spec.Component == nil {
			continue
		}
		out = append(out, supervisor.Spec{
			Name:      name,
			Component: spec.Component,
		})
	}
	return out
}

func classifyHostDependencyError(err error) (retryable bool, fatal bool) {
	switch {
	case err == nil:
		return false, false
	case errors.Is(err, errLongLivedRunServiceClosed):
		return false, true
	case errors.Is(err, errRuntimeComponentHostStopped):
		return false, true
	default:
		return false, true
	}
}
