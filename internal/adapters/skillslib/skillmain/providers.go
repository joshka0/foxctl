package skillmain

import (
	"context"
	"errors"
)

// TryProviders tries each provider in order, returning the first successful
// result. Each attempt is guarded by the named circuit breaker.
//
// The attempt function receives a context and a provider, returning a result
// and error. On success (nil error), TryProviders returns immediately. On
// failure, it records the error and continues to the next provider.
//
// Returns the zero value of T and the last error if all providers fail.
//
// Usage:
//
//	type result struct {
//	    Summary  string
//	    Provider string
//	}
//	r, err := skillmain.TryProviders(rc, skillmain.BreakerLLMProvider, ctx, providers,
//	    func(ctx context.Context, p llmproviders.Provider) (result, error) {
//	        s, e := callLLM(ctx, p, prompt)
//	        return result{Summary: s, Provider: p.Name}, e
//	    },
//	)
func TryProviders[P any, T any](
	rc *RunContext,
	breaker string,
	ctx context.Context,
	providers []P,
	attempt func(context.Context, P) (T, error),
) (T, error) {
	var zero T
	if len(providers) == 0 {
		return zero, errors.New("no providers available")
	}
	var lastErr error
	for _, p := range providers {
		var result T
		err := GuardCall(rc, breaker, ctx, func(ctx context.Context) error {
			var e error
			result, e = attempt(ctx, p)
			return e
		})
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return zero, lastErr
}
