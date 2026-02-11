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

// RetryPolicy configures retry behavior.
type RetryPolicy struct {
	MaxAttempts int           // Total attempts (1 = no retry)
	Backoff     time.Duration // Initial backoff between retries
	Retryable   func(error) bool // Return true to retry; nil = retry all errors
}

// WithRetry retries the skill on transient errors with exponential backoff.
// Note: backoff uses time.After directly (not rc.Now) since retry is inherently I/O-bound.
func WithRetry[I any](policy RetryPolicy) Middleware[I] {
	return func(next RunFunc[I]) RunFunc[I] {
		return func(ctx context.Context, rc *RunContext, in I) error {
			maxAttempts := policy.MaxAttempts
			if maxAttempts <= 0 {
				maxAttempts = 1
			}
			backoff := policy.Backoff
			if backoff <= 0 {
				backoff = time.Second
			}
			var lastErr error
			for attempt := 0; attempt < maxAttempts; attempt++ {
				if attempt > 0 {
					rc.Logger.Warn().Int("attempt", attempt+1).Err(lastErr).Msg("retrying skill")
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(backoff):
					}
					backoff *= 2 // exponential
				}
				lastErr = next(ctx, rc, in)
				if lastErr == nil {
					return nil
				}
				if policy.Retryable != nil && !policy.Retryable(lastErr) {
					return lastErr
				}
			}
			return lastErr
		}
	}
}

// WithInputLog logs a structured summary of the input on entry.
// The summarize function extracts loggable fields (avoid logging sensitive data).
// If summarize is nil, logs only the event name.
func WithInputLog[I any](summarize func(I) map[string]any) Middleware[I] {
	return func(next RunFunc[I]) RunFunc[I] {
		return func(ctx context.Context, rc *RunContext, in I) error {
			ev := rc.Logger.Debug().Str("event", "skill_input")
			if summarize != nil {
				for k, v := range summarize(in) {
					ev = ev.Interface(k, v)
				}
			}
			ev.Msg("input received")
			return next(ctx, rc, in)
		}
	}
}
