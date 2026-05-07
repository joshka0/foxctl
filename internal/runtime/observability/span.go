package observability

import (
	"context"
	"errors"
	"time"
)

// SpanOpt configures a span created by StartSpan/StartSpanAt.
type SpanOpt func(*spanOpts)

type spanOpts struct {
	traceID     string
	parentID    string
	component   string
	command     string
	subtype     string
	sessionID   string
	agentID     string
	workspaceID string
	jobID       string
	data        map[string]any
	persist     *persistConfig // Persistence configuration
}

// WithSpanTraceID sets a specific trace ID for the span.
// Use this when you want to correlate with an existing trace (e.g., correlation_id).
func WithSpanTraceID(id string) SpanOpt {
	return func(o *spanOpts) { o.traceID = id }
}

// WithSpanParentID sets the parent span ID for nested operations.
func WithSpanParentID(id string) SpanOpt {
	return func(o *spanOpts) { o.parentID = id }
}

// WithSpanComponent sets the component that generated the event.
// Common values: ComponentSkill, ComponentCLI, ComponentHook, ComponentJob.
func WithSpanComponent(component string) SpanOpt {
	return func(o *spanOpts) { o.component = component }
}

// WithSpanCommand sets the command/skill/hook name.
func WithSpanCommand(cmd string) SpanOpt {
	return func(o *spanOpts) { o.command = cmd }
}

// WithSpanSubtype sets additional operation classification.
// Use this to differentiate layers (e.g., "runservice" vs "skillmain").
func WithSpanSubtype(subtype string) SpanOpt {
	return func(o *spanOpts) { o.subtype = subtype }
}

// WithSpanSession sets session and agent IDs for business context.
func WithSpanSession(sessionID, agentID string) SpanOpt {
	return func(o *spanOpts) {
		o.sessionID = sessionID
		o.agentID = agentID
	}
}

// WithSpanWorkspace sets the logical workspace ID.
func WithSpanWorkspace(id string) SpanOpt {
	return func(o *spanOpts) { o.workspaceID = id }
}

// WithSpanJobID sets the background job ID.
func WithSpanJobID(id string) SpanOpt {
	return func(o *spanOpts) { o.jobID = id }
}

// WithSpanData adds a key-value pair to the domain-specific data.
func WithSpanData(key string, value any) SpanOpt {
	return func(o *spanOpts) {
		if o.data == nil {
			o.data = make(map[string]any)
		}
		o.data[key] = value
	}
}

// WithSpanDataMap merges a map of key-value pairs into domain-specific data.
func WithSpanDataMap(m map[string]any) SpanOpt {
	return func(o *spanOpts) {
		if len(m) == 0 {
			return
		}
		if o.data == nil {
			o.data = make(map[string]any, len(m))
		}
		for k, v := range m {
			o.data[k] = v
		}
	}
}

// Artifact keys for foxcular events - these enable full request/response replay.
const (
	// ArtifactInput is the CAS digest of the input payload.
	ArtifactInput = "input_artifact"
	// ArtifactResult is the CAS digest of the output/result payload.
	ArtifactResult = "result_artifact"
	// ArtifactStderr is the CAS digest of captured stderr.
	ArtifactStderr = "stderr_artifact"
	// ArtifactTrajectory is the trajectory ID for turn context.
	ArtifactTrajectory = "trajectory_id"
	// ArtifactMailbox is the mailbox message ID for actor coordination.
	ArtifactMailbox = "mailbox_message_id"
)

// WithSpanInputArtifact sets the input CAS digest for request replay.
func WithSpanInputArtifact(digest string) SpanOpt {
	return WithSpanData(ArtifactInput, digest)
}

// WithSpanResultArtifact sets the result CAS digest for response replay.
func WithSpanResultArtifact(digest string) SpanOpt {
	return WithSpanData(ArtifactResult, digest)
}

// WithSpanStderrArtifact sets the stderr CAS digest for debugging.
func WithSpanStderrArtifact(digest string) SpanOpt {
	return WithSpanData(ArtifactStderr, digest)
}

// WithSpanTrajectory sets the trajectory ID for turn correlation.
func WithSpanTrajectory(trajectoryID string) SpanOpt {
	return WithSpanData(ArtifactTrajectory, trajectoryID)
}

// WithSpanMailbox sets the mailbox message ID for actor correlation.
func WithSpanMailbox(mailboxMsgID string) SpanOpt {
	return WithSpanData(ArtifactMailbox, mailboxMsgID)
}

// WithSpanPersistence sets the persistence mode for the span's event.
// Use PersistSQL for high-value events that need queryability.
// Use PersistHybrid for events that need both fast writes and queryability.
func WithSpanPersistence(mode PersistenceMode) SpanOpt {
	return func(o *spanOpts) {
		if o.persist == nil {
			o.persist = &persistConfig{}
		}
		o.persist.mode = mode
	}
}

// WithSpanPersistenceFile sets a custom NDJSON filename for the span's event.
func WithSpanPersistenceFile(name string) SpanOpt {
	return func(o *spanOpts) {
		if o.persist == nil {
			o.persist = &persistConfig{}
		}
		o.persist.fileName = name
	}
}

// StartSpan starts a span "now" and returns:
//   - a context with trace ID attached
//   - a done(err) func that emits one Event on exit
//   - the EventBuilder so callers can add more fields mid-flight
//
// Usage:
//
//	ctx, done, span := observability.StartSpan(ctx, observability.OpSkillRun,
//	    observability.WithSpanComponent(observability.ComponentSkill),
//	    observability.WithSpanCommand("code/snippet_extract"),
//	)
//	defer func() { done(err) }()
//	span.WithData("files", count)
//
// Index:
//
//	Purpose: Start a new span and return a completion callback
//	Flow: call StartSpanAt with time.Now → return context, done, builder
//	Related: StartSpanAt, EventBuilder
//	Keywords: start_span, trace_id, event, done_callback, event_builder
//
// [[protocol:span-lifecycle]]
// [[domain:observability-tracing]]
func StartSpan(ctx context.Context, op string, opts ...SpanOpt) (context.Context, func(error), *EventBuilder) {
	return StartSpanAt(ctx, time.Now(), op, opts...)
}

// StartSpanAt is the same as StartSpan, but uses a caller-provided start time.
// This is useful when you already captured a start time for other reasons.
//
// Index:
//
//	Purpose: Start a span with a caller-provided start time
//	Flow: apply options → ensure trace → build event → enrich context → return done func
//	Related: StartSpan, EmitWithConfig, EnsureTraceID
//	Keywords: start_span_at, trace_id, emit, event, persist_config
//
// [[protocol:span-lifecycle]]
// [[domain:observability-tracing]]
func StartSpanAt(ctx context.Context, startedAt time.Time, op string, opts ...SpanOpt) (context.Context, func(error), *EventBuilder) {
	var o spanOpts
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	// If caller wants a specific trace ID (e.g., correlation ID), make it authoritative.
	if o.traceID != "" {
		ctx = WithTraceID(ctx, o.traceID)
	}

	ctx, traceID := EnsureTraceID(ctx)

	b := NewEvent(op).WithTraceID(traceID)
	ctx = WithSpanID(ctx, b.event.SpanID)
	if o.parentID != "" {
		b = b.WithParentID(o.parentID)
	}
	if o.component != "" {
		b = b.WithComponent(o.component)
	}
	if o.command != "" {
		b = b.WithCommand(o.command)
	}
	if o.subtype != "" {
		b = b.WithSubtype(o.subtype)
	}
	if o.sessionID != "" || o.agentID != "" {
		b = b.WithSession(o.sessionID, o.agentID)
	}
	if o.workspaceID != "" {
		b = b.WithWorkspace(o.workspaceID)
	}
	if o.jobID != "" {
		b = b.WithJobID(o.jobID)
	}
	if len(o.data) > 0 {
		b = b.WithDataMap(o.data)
	}
	if o.persist != nil {
		b.persist = o.persist
	}

	// Fill missing business context from env/context.
	b = b.EnrichFromEnv().EnrichFromContext(ctx)

	done := func(err error) {
		dur := time.Since(startedAt)
		var event *Event
		switch {
		case err == nil:
			event = b.Success(dur)
		case errors.Is(err, context.Canceled):
			event = b.Canceled(dur)
		default:
			event = b.Error(err, dur)
		}
		EmitWithConfig(ctx, event, b.persist)
	}

	return ctx, done, b
}
