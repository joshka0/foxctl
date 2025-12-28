package runservice

import (
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
)

// emitWideEvent creates and emits a wide event for skill execution.
// This captures the full operation context in a single comprehensive event.
func (e *Executor) emitWideEvent(startTime time.Time, source string, cacheHit bool, err error) {
	builder := observability.NewEvent(observability.OpSkillRun).
		WithComponent(observability.ComponentSkill).
		WithCommand(e.handle.Manifest.Metadata.Name).
		WithWorkspace(e.options.Workspace).
		EnrichFromEnv().
		EnrichFromContext(e.ctx)

	// Add job context if available
	if e.options.CorrelationID != "" {
		builder = builder.WithData("correlation_id", e.options.CorrelationID)
	}

	// Add domain-specific data
	builder = builder.
		WithData("source", source).
		WithData("cache_hit", cacheHit).
		WithData("skill_version", e.handle.Manifest.Metadata.Version)

	// Add session context from env
	if sessionID := os.Getenv("AGENTCTL_SESSION_ID"); sessionID != "" {
		builder = builder.WithSession(sessionID, os.Getenv("AGENTCTL_AGENT_ID"))
	}

	duration := time.Since(startTime)
	var event *observability.WideEvent
	if err != nil {
		event = builder.Error(err, duration)
	} else {
		event = builder.Success(duration)
	}

	observability.Emit(e.ctx, event)
}

// EmitCacheHitEvent emits a wide event for a cache hit.
func (e *Executor) EmitCacheHitEvent(startTime time.Time) {
	e.emitWideEvent(startTime, "cache", true, nil)
}

// EmitRunEvent emits a wide event for a skill execution (cache miss).
func (e *Executor) EmitRunEvent(startTime time.Time, err error) {
	e.emitWideEvent(startTime, "run", false, err)
}

// EmitAsyncSubmitEvent emits a wide event for an async job submission.
func (e *Executor) EmitAsyncSubmitEvent(startTime time.Time, jobID string, err error) {
	builder := observability.NewEvent(observability.OpJobSubmit).
		WithComponent(observability.ComponentJob).
		WithCommand(e.handle.Manifest.Metadata.Name).
		WithWorkspace(e.options.Workspace).
		WithJobID(jobID).
		EnrichFromEnv().
		EnrichFromContext(e.ctx)

	duration := time.Since(startTime)
	var event *observability.WideEvent
	if err != nil {
		event = builder.Error(err, duration)
	} else {
		event = builder.Success(duration)
	}

	observability.Emit(e.ctx, event)
}
