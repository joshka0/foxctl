package lite

import (
	"context"
	"fmt"
	"time"
)

// Middleware wraps a lite RunFunc to add cross-cutting behavior.
// Middleware is applied outermost-first: the first middleware in a Chain
// call is the first to see the request and the last to see the response.
type Middleware[I any] func(RunFunc[I]) RunFunc[I]

// Chain composes middleware around a RunFunc. Middleware is applied in order:
// the first argument wraps outermost, executing first on entry and last on exit.
//
// Usage:
//
//	func main() {
//	    lite.Main("code/symbols", lite.Chain(run,
//	        lite.WithTimeout[Input](30*time.Second),
//	        lite.WithRecover[Input](),
//	    ))
//	}
func Chain[I any](run RunFunc[I], mw ...Middleware[I]) RunFunc[I] {
	for i := len(mw) - 1; i >= 0; i-- {
		run = mw[i](run)
	}
	return run
}

// WithTimeout adds a context deadline to the skill execution.
// If the skill does not complete within d, the context is canceled.
func WithTimeout[I any](d time.Duration) Middleware[I] {
	return func(next RunFunc[I]) RunFunc[I] {
		return func(ctx context.Context, rc *RunContext, in I) error {
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return next(ctx, rc, in)
		}
	}
}

// WithRecover catches panics in the skill and converts them to errors.
func WithRecover[I any]() Middleware[I] {
	return func(next RunFunc[I]) RunFunc[I] {
		return func(ctx context.Context, rc *RunContext, in I) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("skill panicked: %v", r)
				}
			}()
			return next(ctx, rc, in)
		}
	}
}

// WithDynamicTimeout extracts a timeout duration from the input struct.
// If getDuration returns 0, no timeout is applied.
func WithDynamicTimeout[I any](getDuration func(I) time.Duration) Middleware[I] {
	return func(next RunFunc[I]) RunFunc[I] {
		return func(ctx context.Context, rc *RunContext, in I) error {
			d := getDuration(in)
			if d > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, d)
				defer cancel()
			}
			return next(ctx, rc, in)
		}
	}
}
