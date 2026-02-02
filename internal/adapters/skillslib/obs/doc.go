// Package obs provides observability helpers for skills.
// It wraps internal/observability to provide a clean abstraction layer,
// allowing skills to emit wide events without importing internal packages directly.
// It also tracks LLM token usage and costs for skill telemetry.
//
// Usage:
//
//	// Start a span for an operation
//	ctx, done, span := obs.StartSpan(ctx, "my_operation",
//	    obs.WithCommand("my/skill"),
//	    obs.WithData("input_count", 10),
//	)
//	defer func() { done(err) }()
//
//	// Add data during operation
//	span.WithData("processed", count)
//
//	// Or emit a standalone event
//	obs.Emit(ctx, obs.NewEvent("cache.hit").
//	    WithCommand("my/skill").
//	    WithData("key", cacheKey).
//	    Success(time.Since(start)))
package obs
