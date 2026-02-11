package skillmain

import (
	"context"
	"fmt"
	"time"
)

// Middleware wraps a RunFunc to add cross-cutting behavior.
// Middleware is applied outermost-first: the first middleware in a Chain
// call is the first to see the request and the last to see the response.
type Middleware[I any] func(RunFunc[I]) RunFunc[I]

// Chain composes middleware around a RunFunc. Middleware is applied in order:
// the first argument wraps outermost, executing first on entry and last on exit.
//
// Usage:
//
//	func main() {
//	    skillmain.Main("code/symbols", skillmain.Chain(run,
//	        skillmain.WithTimeout[Input](30*time.Second),
//	        skillmain.WithRecover[Input](),
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

// WithSkillStep wraps the entire run function in a Step log entry,
// automatically recording total execution time under the given name.
func WithSkillStep[I any](name string) Middleware[I] {
	return func(next RunFunc[I]) RunFunc[I] {
		return func(ctx context.Context, rc *RunContext, in I) error {
			done := Step(rc, name)
			err := next(ctx, rc, in)
			done(err)
			return err
		}
	}
}
