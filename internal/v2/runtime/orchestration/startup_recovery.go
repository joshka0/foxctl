package orchestration

import "context"

// StartupRecovery repairs projected running work before a scheduler dispatch cycle.
type StartupRecovery interface {
	RecoverOrphanedRuns(ctx context.Context) error
}

// StartupRecoveryFunc adapts a function into StartupRecovery.
type StartupRecoveryFunc func(ctx context.Context) error

// RecoverOrphanedRuns runs one startup recovery pass.
func (f StartupRecoveryFunc) RecoverOrphanedRuns(ctx context.Context) error {
	if f == nil {
		return nil
	}
	return f(ctx)
}
