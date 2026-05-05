package foxcular

import (
	"context"
	"log/slog"
)

// SlogHandler is a slog.Handler that emits foxcular events via a foxcular Client.
//
// When the provided context carries an active foxcular span, the slog record
// is attached to that span as a correlated event (preserving trace and span
// IDs). When no span is active, a standalone foxcular event is emitted instead.
//
// All records are subject to the Client's redaction policy before delivery.
type SlogHandler struct {
	client  *Client
	groups  []string
	attrs   []slog.Attr
	options *SlogHandlerOptions
}

// SlogHandlerOptions configures the behaviour of a SlogHandler.
type SlogHandlerOptions struct {
	// Level controls which log levels are handled. If nil, all levels are handled.
	Level slog.Leveler

	// Operation is the event operation name. Defaults to "slog".
	Operation string
}

// NewSlogHandler creates a slog.Handler backed by the given foxcular Client.
func NewSlogHandler(client *Client, opts *SlogHandlerOptions) *SlogHandler {
	if opts == nil {
		opts = &SlogHandlerOptions{}
	}
	if opts.Operation == "" {
		opts.Operation = "slog"
	}
	return &SlogHandler{
		client:  client,
		options: opts,
	}
}

// Enabled returns true if the handler should emit records at the given level.
func (h *SlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	if h.options.Level != nil {
		return level >= h.options.Level.Level()
	}
	return true
}

// Handle processes a single slog record and emits it as a foxcular event.
func (h *SlogHandler) Handle(ctx context.Context, r slog.Record) error {
	operation := h.options.Operation

	// Flatten attrs and groups into a data map.
	data := h.flattenAttrs(r)
	data["slog.level"] = r.Level.String()

	// Build the event directly using the builder, then set status.
	builder := h.client.Emit(operation)

	// Set message.
	builder.builder.WithMessage(r.Message)
	builder.builder.WithName(r.Message)

	// Set data fields.
	for k, v := range data {
		builder.builder.WithData(k, v)
	}

	// Correlate with active span if present.
	if span := ActiveSpanFromContext(ctx); span != nil {
		builder.builder.WithTraceID(span.TraceID())
		builder.builder.WithParentID(span.SpanID())
	}

	// Determine status from level and finalize.
	switch {
	case r.Level >= slog.LevelError:
		event := builder.builder.Error(nil, 0)
		event.ErrorMessage = r.Message
		return h.client.EmitEventSync(ctx, event)
	default:
		event := builder.builder.Success(0)
		return h.client.EmitEventSync(ctx, event)
	}
}

// WithAttrs returns a new handler with the given attributes pre-added.
func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cp := *h
	cp.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &cp
}

// WithGroup returns a new handler scoped to the named group.
func (h *SlogHandler) WithGroup(name string) slog.Handler {
	cp := *h
	cp.groups = append(append([]string{}, h.groups...), name)
	return &cp
}

// flattenAttrs merges pre-accumulated attrs, record attrs, and groups into a
// single data map with group-prefixed keys.
func (h *SlogHandler) flattenAttrs(r slog.Record) map[string]any {
	data := make(map[string]any)

	prefix := ""
	if len(h.groups) > 0 {
		for _, g := range h.groups {
			if prefix != "" {
				prefix += "."
			}
			prefix += g
		}
	}

	addAttr := func(attr slog.Attr) {
		key := attr.Key
		if prefix != "" {
			key = prefix + "." + key
		}
		data[key] = resolveSlogValue(attr.Value)
	}

	// Pre-accumulated attrs.
	for _, attr := range h.attrs {
		addAttr(attr)
	}

	// Record-level attrs.
	r.Attrs(func(attr slog.Attr) bool {
		addAttr(attr)
		return true
	})

	return data
}

// resolveSlogValue extracts the Go value from a slog.Value.
func resolveSlogValue(v slog.Value) any {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindTime:
		return v.Time()
	case slog.KindDuration:
		return v.Duration()
	case slog.KindGroup:
		group := v.Group()
		m := make(map[string]any, len(group))
		for _, attr := range group {
			m[attr.Key] = resolveSlogValue(attr.Value)
		}
		return m
	case slog.KindAny:
		return v.Any()
	default:
		return v.Any()
	}
}
