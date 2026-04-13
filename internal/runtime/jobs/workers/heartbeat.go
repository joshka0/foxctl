package workers

import (
	"context"
	"fmt"
	"time"

	"github.com/jkatigb/agentctl/internal/runtime/observability"
	"github.com/riverqueue/river"
)

const (
	agentHeartbeatCheckKind  = "agent.heartbeat_check"
	defaultStaleHeartbeatAge = 2 * time.Minute
)

// AgentHeartbeatCheckArgs contains arguments for stale-agent heartbeat checks.
type AgentHeartbeatCheckArgs struct{}

// Kind returns the River job kind.
func (AgentHeartbeatCheckArgs) Kind() string { return agentHeartbeatCheckKind }

// StaleAgentRecoverer recovers stale agents based on a heartbeat age threshold.
type StaleAgentRecoverer interface {
	// RecoverStaleAgents finds stale agents and transitions them to a recovery state.
	RecoverStaleAgents(ctx context.Context, staleAfter time.Duration) (int, error)
}

// AgentHeartbeatCheckWorker performs stale-agent heartbeat recovery checks.
type AgentHeartbeatCheckWorker struct {
	river.WorkerDefaults[AgentHeartbeatCheckArgs]

	// Recoverer handles stale-agent recovery logic.
	Recoverer StaleAgentRecoverer

	// StaleAfter overrides the stale heartbeat threshold.
	// If zero or negative, a default of 2 minutes is used.
	StaleAfter time.Duration
}

// Work runs a heartbeat recovery check.
func (w *AgentHeartbeatCheckWorker) Work(ctx context.Context, job *river.Job[AgentHeartbeatCheckArgs]) error {
	if w == nil {
		return fmt.Errorf("jobs: heartbeat worker is nil")
	}
	if job == nil {
		return fmt.Errorf("jobs: heartbeat job is required")
	}
	if w.Recoverer == nil {
		return fmt.Errorf("jobs: stale agent recoverer is required")
	}

	staleAfter := w.StaleAfter
	if staleAfter <= 0 {
		staleAfter = defaultStaleHeartbeatAge
	}

	recovered, err := w.Recoverer.RecoverStaleAgents(ctx, staleAfter)
	event := observability.NewEvent("jobs.agent_heartbeat_check").
		WithComponent(observability.ComponentJob).
		WithData("stale_after_seconds", int(staleAfter.Seconds())).
		WithData("recovered", recovered)
	if err != nil {
		wrappedErr := fmt.Errorf("jobs: recover stale agents: %w", err)
		observability.Emit(ctx, event.Error(wrappedErr, 0))
		return wrappedErr
	}

	observability.Emit(ctx, event.Success(0))
	return nil
}

var _ river.Worker[AgentHeartbeatCheckArgs] = (*AgentHeartbeatCheckWorker)(nil)
