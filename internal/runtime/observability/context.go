package observability

import (
	"context"

	platformobs "github.com/joshka0/foxctl/internal/platform/observability"
)

// EnvTraceID is the environment variable for propagating trace IDs.
const EnvTraceID = platformobs.EnvTraceID

// NewTraceID generates a new unique trace ID using ULID.
func NewTraceID() string {
	return platformobs.NewTraceID()
}

// WithTraceID attaches a trace ID to the context for propagation.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return platformobs.WithTraceID(ctx, traceID)
}

// TraceIDFromContext retrieves the trace ID from context.
func TraceIDFromContext(ctx context.Context) string {
	return platformobs.TraceIDFromContext(ctx)
}

// WithSpanID attaches a span ID to the context.
func WithSpanID(ctx context.Context, spanID string) context.Context {
	return platformobs.WithSpanID(ctx, spanID)
}

// SpanIDFromContext retrieves the span ID from context.
func SpanIDFromContext(ctx context.Context) string {
	return platformobs.SpanIDFromContext(ctx)
}

// EnsureTraceID returns the trace ID from context, or generates and attaches a new one.
func EnsureTraceID(ctx context.Context) (context.Context, string) {
	return platformobs.EnsureTraceID(ctx)
}

// PropagationEnv returns environment variables to propagate trace context to child processes.
func PropagationEnv(ctx context.Context) []string {
	return platformobs.PropagationEnv(ctx)
}
