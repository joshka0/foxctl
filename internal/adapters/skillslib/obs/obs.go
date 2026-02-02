package obs

import (
	"context"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
)

// Re-export types for skill use
type (
	// EventBuilder provides a fluent API for constructing wide events.
	EventBuilder = observability.EventBuilder

	// WideEvent is a comprehensive observability event.
	WideEvent = observability.WideEvent

	// SpanOpt configures a span created by StartSpan.
	SpanOpt = observability.SpanOpt

	// Sampler determines whether an event should be recorded.
	Sampler = observability.Sampler

	// Status represents the outcome of an operation.
	Status = observability.Status

	// PersistenceMode determines how an event is persisted.
	PersistenceMode = observability.PersistenceMode
)

// Status constants
const (
	StatusOK       = observability.StatusOK
	StatusError    = observability.StatusError
	StatusCanceled = observability.StatusCanceled
)

// Persistence mode constants
const (
	// PersistDefault uses the global default (NDJSON file).
	PersistDefault = observability.PersistDefault
	// PersistNDJSON writes to the default NDJSON file.
	PersistNDJSON = observability.PersistNDJSON
	// PersistSQL writes directly to SQLite (blocking).
	PersistSQL = observability.PersistSQL
	// PersistHybrid writes to NDJSON immediately, queues for SQLite sync.
	PersistHybrid = observability.PersistHybrid
	// PersistNone disables persistence (still sampled/logged).
	PersistNone = observability.PersistNone
)

// Component constants - use these for WithComponent/WithSpanComponent
const (
	ComponentSkill = observability.ComponentSkill
	ComponentCLI   = observability.ComponentCLI
	ComponentHook  = observability.ComponentHook
	ComponentJob   = observability.ComponentJob
	ComponentWeb   = observability.ComponentWeb
)

// Operation constants - standard operation names
const (
	OpSkillRun    = observability.OpSkillRun
	OpSkillCache  = observability.OpSkillCache
	OpHookExecute = observability.OpHookExecute
	OpJobSubmit   = observability.OpJobSubmit
	OpJobComplete = observability.OpJobComplete
)

// Data key constants - standard keys for the data map
const (
	// Artifact keys for replay capability
	KeyInputArtifact  = observability.ArtifactInput
	KeyResultArtifact = observability.ArtifactResult
	KeyStderrArtifact = observability.ArtifactStderr
	KeyTrajectoryID   = observability.ArtifactTrajectory
	KeyMailboxMsgID   = observability.ArtifactMailbox

	// Common metric keys
	KeyCacheHit      = "cache_hit"
	KeyFilesCount    = "files"
	KeyCandidates    = "candidates"
	KeyDurationMS    = "duration_ms"
	KeySource        = "source"
	KeyErrorType     = "error_type"
	KeyRetryCount    = "retry_count"
	KeyTokensUsed    = "tokens_used"
	KeyModelName     = "model"
	KeyProvider      = "provider"
	KeyResultCount   = "result_count"
	KeySkipped       = "skipped"
	KeyProcessed     = "processed"
	KeyBytesRead     = "bytes_read"
	KeyBytesWritten  = "bytes_written"
	KeyLinesCount    = "lines"
	KeySnippetsCount = "snippets"
)

// NewEvent creates a new EventBuilder for the specified operation.
// The operation should describe what is being done (e.g., "cache.lookup", "file.parse").
func NewEvent(operation string) *EventBuilder {
	return observability.NewEvent(operation)
}

// StartSpan starts a span and returns a context, done function, and event builder.
// Call done(err) when the operation completes to emit the wide event.
//
// Usage:
//
//	ctx, done, span := obs.StartSpan(ctx, "database.query",
//	    obs.WithCommand("my/skill"),
//	    obs.WithData("table", "users"),
//	)
//	defer func() { done(err) }()
//	span.WithData("rows", rowCount)
func StartSpan(ctx context.Context, operation string, opts ...SpanOpt) (context.Context, func(error), *EventBuilder) {
	return observability.StartSpan(ctx, operation, opts...)
}

// StartSpanAt is like StartSpan but uses a caller-provided start time.
func StartSpanAt(ctx context.Context, startedAt time.Time, operation string, opts ...SpanOpt) (context.Context, func(error), *EventBuilder) {
	return observability.StartSpanAt(ctx, startedAt, operation, opts...)
}

// Emit writes a wide event to the observability stream.
// Events are sampled according to the configured sampler.
// This is safe to call from any goroutine.
func Emit(ctx context.Context, event *WideEvent) {
	observability.Emit(ctx, event)
}

// EmitSync writes a wide event synchronously, bypassing sampling.
// Use this for critical events that must always be recorded.
func EmitSync(ctx context.Context, event *WideEvent) error {
	return observability.EmitSync(ctx, event)
}

// Span options - re-exported for convenience

// WithTraceID sets a specific trace ID for correlation.
func WithTraceID(id string) SpanOpt {
	return observability.WithSpanTraceID(id)
}

// WithParentID sets the parent span ID for nested operations.
func WithParentID(id string) SpanOpt {
	return observability.WithSpanParentID(id)
}

// WithComponent sets the component (skill, hook, job, etc.).
func WithComponent(component string) SpanOpt {
	return observability.WithSpanComponent(component)
}

// WithCommand sets the skill/hook/command name.
func WithCommand(cmd string) SpanOpt {
	return observability.WithSpanCommand(cmd)
}

// WithSubtype sets additional operation classification.
func WithSubtype(subtype string) SpanOpt {
	return observability.WithSpanSubtype(subtype)
}

// WithSession sets session and agent IDs.
func WithSession(sessionID, agentID string) SpanOpt {
	return observability.WithSpanSession(sessionID, agentID)
}

// WithWorkspace sets the workspace ID.
func WithWorkspace(id string) SpanOpt {
	return observability.WithSpanWorkspace(id)
}

// WithJobID sets the background job ID.
func WithJobID(id string) SpanOpt {
	return observability.WithSpanJobID(id)
}

// WithData adds a key-value pair to the event data.
func WithData(key string, value any) SpanOpt {
	return observability.WithSpanData(key, value)
}

// WithDataMap merges multiple key-value pairs into event data.
func WithDataMap(m map[string]any) SpanOpt {
	return observability.WithSpanDataMap(m)
}

// WithInputArtifact sets the CAS digest of the input payload.
func WithInputArtifact(digest string) SpanOpt {
	return observability.WithSpanInputArtifact(digest)
}

// WithResultArtifact sets the CAS digest of the output payload.
func WithResultArtifact(digest string) SpanOpt {
	return observability.WithSpanResultArtifact(digest)
}

// WithPersistence sets the persistence mode for the span's event.
// Use PersistSQL for high-value events that need queryability.
// Use PersistHybrid for events that need both fast writes and queryability.
func WithPersistence(mode PersistenceMode) SpanOpt {
	return observability.WithSpanPersistence(mode)
}

// WithPersistenceFile sets a custom NDJSON filename for the span's event.
func WithPersistenceFile(name string) SpanOpt {
	return observability.WithSpanPersistenceFile(name)
}

// Context helpers

// TraceIDFromContext retrieves the trace ID from context.
func TraceIDFromContext(ctx context.Context) string {
	return observability.TraceIDFromContext(ctx)
}

// WithTraceIDContext attaches a trace ID to the context.
func WithTraceIDContext(ctx context.Context, traceID string) context.Context {
	return observability.WithTraceID(ctx, traceID)
}

// EnsureTraceID gets or generates a trace ID for the context.
func EnsureTraceID(ctx context.Context) (context.Context, string) {
	return observability.EnsureTraceID(ctx)
}

// PropagationEnv returns environment variables for trace propagation to child processes.
func PropagationEnv(ctx context.Context) []string {
	return observability.PropagationEnv(ctx)
}

// Utility functions

// HashQuestion returns a truncated SHA-256 hash of a question/query.
// Useful for correlation without storing PII.
func HashQuestion(q string) string {
	return observability.HashQuestion(q)
}

// SetSamplerForTesting overrides the sampler for testing purposes.
func SetSamplerForTesting(s Sampler) {
	observability.SetSamplerForTesting(s)
}
