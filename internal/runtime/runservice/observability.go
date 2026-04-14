package runservice

import (
	"encoding/json"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/maputil"
	"github.com/jkatigb/agentctl/internal/runtime/observability"
)

// resultArtifacts holds CAS digests extracted from execution results.
type resultArtifacts struct {
	Result string // result_artifact from cas_digest in meta
}

// extractArtifacts extracts CAS digests from a result envelope.
func extractArtifacts(result []byte) resultArtifacts {
	if len(result) == 0 {
		return resultArtifacts{}
	}

	var env map[string]any
	if err := json.Unmarshal(result, &env); err != nil {
		return resultArtifacts{}
	}

	meta, ok := maputil.AsStringMap(env["meta"])
	if !ok {
		return resultArtifacts{}
	}

	var artifacts resultArtifacts
	if digest, ok := meta["cas_digest"].(string); ok {
		artifacts.Result = digest
	}

	return artifacts
}

// emitWideEvent creates and emits a wide event for skill execution.
// This captures the full operation context in a single comprehensive event.
// Uses StartSpanAt so trace_id follows correlation_id when available.
func (e *Executor) emitWideEvent(startTime time.Time, source string, cacheHit bool, err error) {
	e.emitWideEventWithArtifacts(startTime, source, cacheHit, err, resultArtifacts{})
}

// emitWideEventWithArtifacts creates and emits a wide event with CAS artifacts.
func (e *Executor) emitWideEventWithArtifacts(startTime time.Time, source string, cacheHit bool, err error, artifacts resultArtifacts) {
	// Use correlation_id as trace_id when available for cross-layer correlation.
	_, done, builder := observability.StartSpanAt(
		e.ctx,
		startTime,
		observability.OpSkillRun,
		observability.WithSpanTraceID(e.options.CorrelationID), // Makes correlation_id the trace_id
		observability.WithSpanComponent(observability.ComponentSkill),
		observability.WithSpanCommand(e.handle.Manifest.Metadata.Name),
		observability.WithSpanWorkspace(e.options.Workspace),
		observability.WithSpanSubtype("runservice"),
	)

	// Add domain-specific data
	if e.options.CorrelationID != "" {
		builder.WithData("correlation_id", e.options.CorrelationID)
	}
	builder.WithData("source", source)
	builder.WithData("cache_hit", cacheHit)
	builder.WithData("skill_version", e.handle.Manifest.Metadata.Version)

	// Add CAS artifact digests for full replay capability
	if artifacts.Result != "" {
		builder.WithResultArtifact(artifacts.Result)
	}

	// done() handles success/error/canceled and emits the event.
	done(err)
}

// EmitCacheHitEvent emits a wide event for a cache hit.
func (e *Executor) EmitCacheHitEvent(startTime time.Time) {
	e.emitWideEvent(startTime, "cache", true, nil)
}

// EmitRunEvent emits a wide event for a skill execution (cache miss).
func (e *Executor) EmitRunEvent(startTime time.Time, err error) {
	e.emitWideEvent(startTime, "run", false, err)
}

// EmitRunEventWithResult emits a wide event with result artifacts.
// Use this when you have access to the result bytes to extract CAS digests.
func (e *Executor) EmitRunEventWithResult(startTime time.Time, result []byte, err error) {
	artifacts := extractArtifacts(result)
	e.emitWideEventWithArtifacts(startTime, "run", false, err, artifacts)
}

// EmitAsyncSubmitEvent emits a wide event for an async job submission.
func (e *Executor) EmitAsyncSubmitEvent(startTime time.Time, jobID string, err error) {
	_, done, _ := observability.StartSpanAt(
		e.ctx,
		startTime,
		observability.OpJobSubmit,
		observability.WithSpanTraceID(e.options.CorrelationID),
		observability.WithSpanComponent(observability.ComponentJob),
		observability.WithSpanCommand(e.handle.Manifest.Metadata.Name),
		observability.WithSpanWorkspace(e.options.Workspace),
		observability.WithSpanJobID(jobID),
		observability.WithSpanSubtype("runservice"),
	)
	done(err)
}
