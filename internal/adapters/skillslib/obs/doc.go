// Package obs provides reusable observability helpers for skills. It belongs to
// the generic skillslib tooling-support family while wrapping
// internal/observability to keep skill code runtime-neutral. It also tracks
// LLM token usage and costs for skill telemetry.
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
