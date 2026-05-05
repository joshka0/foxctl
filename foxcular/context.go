package foxcular

import "context"

type (
	traceIDKey    struct{}
	spanIDKey     struct{}
	activeSpanKey struct{}
)

// WithTraceID attaches a trace ID to the context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// TraceIDFromContext retrieves the trace ID from context.
// Returns empty string if not found.
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(traceIDKey{}).(string); ok && id != "" {
		return id
	}
	return ""
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

// WithActiveSpan attaches an active Span to the context.
func WithActiveSpan(ctx context.Context, s *Span) context.Context {
	return context.WithValue(ctx, activeSpanKey{}, s)
}

// ActiveSpanFromContext retrieves the active Span from context.
func ActiveSpanFromContext(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}
	if s, ok := ctx.Value(activeSpanKey{}).(*Span); ok {
		return s
	}
	return nil
}
