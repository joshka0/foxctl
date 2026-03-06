package orchestration

import "context"

// Reconciler performs one reconcile pass.
type Reconciler interface {
	Reconcile(ctx context.Context) error
}

// ReconcileFunc adapts a function into a Reconciler.
type ReconcileFunc func(ctx context.Context) error

// Reconcile executes one reconcile pass.
func (f ReconcileFunc) Reconcile(ctx context.Context) error {
	if f == nil {
		return nil
	}
	return f(ctx)
}
