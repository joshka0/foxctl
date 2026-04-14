package workers

import (
	"context"
	"fmt"

	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/riverqueue/river"
)

const agentIndexCleanupKind = "agent.index_cleanup"

// AgentIndexCleanupArgs contains arguments for agent index cleanup jobs.
type AgentIndexCleanupArgs struct{}

// Kind returns the River job kind.
func (AgentIndexCleanupArgs) Kind() string { return agentIndexCleanupKind }

// AgentIndexCleaner performs cleanup of stale or unused agent index entries.
type AgentIndexCleaner interface {
	// CleanupAgentIndexes runs one cleanup pass and returns deleted entry count.
	CleanupAgentIndexes(ctx context.Context) (int, error)
}

// AgentIndexCleanupWorker runs periodic agent index cleanup.
type AgentIndexCleanupWorker struct {
	river.WorkerDefaults[AgentIndexCleanupArgs]

	// Cleaner performs the cleanup operation.
	Cleaner AgentIndexCleaner
}

// Work executes one cleanup pass.
func (w *AgentIndexCleanupWorker) Work(ctx context.Context, job *river.Job[AgentIndexCleanupArgs]) error {
	if w == nil {
		return fmt.Errorf("jobs: index cleanup worker is nil")
	}
	if job == nil {
		return fmt.Errorf("jobs: index cleanup job is required")
	}
	if w.Cleaner == nil {
		return fmt.Errorf("jobs: index cleaner is required")
	}

	deleted, err := w.Cleaner.CleanupAgentIndexes(ctx)
	event := observability.NewEvent("jobs.agent_index_cleanup").
		WithComponent(observability.ComponentJob).
		WithData("deleted", deleted)
	if err != nil {
		wrappedErr := fmt.Errorf("jobs: cleanup agent indexes: %w", err)
		observability.Emit(ctx, event.Error(wrappedErr, 0))
		return wrappedErr
	}

	observability.Emit(ctx, event.Success(0))
	return nil
}

var _ river.Worker[AgentIndexCleanupArgs] = (*AgentIndexCleanupWorker)(nil)
