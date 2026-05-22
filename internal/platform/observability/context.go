package observability

import (
	"context"
	"os"

	"github.com/oklog/ulid/v2"
)

type (
	traceIDKey struct{}
	spanIDKey  struct{}
)

// EnvTraceID is the environment variable for propagating trace IDs.
const EnvTraceID = "FOXCTL_TRACE_ID"

func NewTraceID() string {
	return ulid.Make().String()
}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return os.Getenv(EnvTraceID)
	}
	if id, ok := ctx.Value(traceIDKey{}).(string); ok && id != "" {
		return id
	}
	return os.Getenv(EnvTraceID)
}

func WithSpanID(ctx context.Context, spanID string) context.Context {
	return context.WithValue(ctx, spanIDKey{}, spanID)
}

func SpanIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(spanIDKey{}).(string); ok {
		return id
	}
	return ""
}

func EnsureTraceID(ctx context.Context) (context.Context, string) {
	if traceID := TraceIDFromContext(ctx); traceID != "" {
		return ctx, traceID
	}
	traceID := NewTraceID()
	return WithTraceID(ctx, traceID), traceID
}

func PropagationEnv(ctx context.Context) []string {
	var env []string
	if traceID := TraceIDFromContext(ctx); traceID != "" {
		env = append(env, EnvTraceID+"="+traceID)
	}
	return env
}
