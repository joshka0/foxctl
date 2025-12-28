package observability

import (
	"context"
	"os"

	"github.com/oklog/ulid/v2"
)

// Context keys for observability data.
type traceIDKey struct{}
type spanIDKey struct{}

// EnvTraceID is the environment variable for propagating trace IDs.
const EnvTraceID = "AGENTCTL_TRACE_ID"

// NewTraceID generates a new unique trace ID using ULID.
func NewTraceID() string {
	return ulid.Make().String()
}

// WithTraceID attaches a trace ID to the context for propagation.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// TraceIDFromContext retrieves the trace ID from context.
// Falls back to AGENTCTL_TRACE_ID env var if not in context.
// Returns empty string if not found.
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return os.Getenv(EnvTraceID)
	}
	if id, ok := ctx.Value(traceIDKey{}).(string); ok && id != "" {
		return id
	}
	return os.Getenv(EnvTraceID)
}

// WithSpanID attaches a span ID to the context.
func WithSpanID(ctx context.Context, spanID string) context.Context {
	return context.WithValue(ctx, spanIDKey{}, spanID)
}

// SpanIDFromContext retrieves the span ID from context.
func SpanIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(spanIDKey{}).(string); ok {
		return id
	}
	return ""
}

// EnsureTraceID returns the trace ID from context, or generates and attaches a new one.
// This is useful at operation entry points to ensure tracing is enabled.
func EnsureTraceID(ctx context.Context) (context.Context, string) {
	if traceID := TraceIDFromContext(ctx); traceID != "" {
		return ctx, traceID
	}
	traceID := NewTraceID()
	return WithTraceID(ctx, traceID), traceID
}

// PropagationEnv returns environment variables to propagate trace context to child processes.
func PropagationEnv(ctx context.Context) []string {
	var env []string
	if traceID := TraceIDFromContext(ctx); traceID != "" {
		env = append(env, EnvTraceID+"="+traceID)
	}
	return env
}
