// Package otlp provides an OTLP log drain that maps foxcular foxcular events to
// OpenTelemetry log records. This is the default OTLP mapping surface.
//
// The drain uses the OpenTelemetry Go SDK log pipeline to deliver events as
// OTLP log records through a configured sdk/log.Exporter. This keeps the core
// foxcular package dependency-light while providing first-class OTLP support
// as an optional integration.
package otlp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/joshka0/foxcular"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/log/logtest"
	"go.opentelemetry.io/otel/trace"
)

// LogExporter wraps an OTEL sdk/log.Exporter and adapts foxcular events into
// OTLP log records. It implements the foxcular.Drain interface.
//
// Events are mapped to OTLP log records preserving severity, body, timestamp,
// trace/span correlation IDs, and custom fields as log attributes.
type LogExporter struct {
	exporter sdklog.Exporter
	opts     LogExporterOptions
	mu       sync.Mutex
	closed   bool
}

// LogExporterOptions configures the OTLP log drain behavior.
type LogExporterOptions struct {
	// ResourceAttrs are included as resource-level attributes on every
	// exported log record (e.g., service.name, service.version).
	ResourceAttrs map[string]string

	// OnError is called when the underlying exporter returns an error during
	// Export. If nil, errors are silently swallowed (best-effort delivery).
	// The callback must not block.
	OnError func(err error)
}

// NewLogExporter creates a new OTLP log drain backed by the given
// sdk/log.Exporter. The exporter is responsible for actual delivery (e.g.,
// gRPC, HTTP, or in-process fake).
func NewLogExporter(exporter sdklog.Exporter, opts *LogExporterOptions) *LogExporter {
	if opts == nil {
		opts = &LogExporterOptions{}
	}
	return &LogExporter{
		exporter: exporter,
		opts:     *opts,
	}
}

// Send maps a foxcular event to an OTLP log record and exports it.
// Errors from the underlying exporter are reported through OnError but do not
// cause a panic.
func (d *LogExporter) Send(ctx context.Context, event *foxcular.Event) error {
	if event == nil {
		return nil
	}

	record := mapEventToRecord(event)

	if err := d.exporter.Export(ctx, []sdklog.Record{record}); err != nil {
		d.reportError(err)
		return err
	}
	return nil
}

// Flush forces delivery of any buffered log records.
func (d *LogExporter) Flush(ctx context.Context) error {
	d.mu.Lock()
	exporter := d.exporter
	d.mu.Unlock()

	if exporter == nil || d.closed {
		return nil
	}
	return exporter.ForceFlush(ctx)
}

// Close shuts down the underlying exporter and releases resources.
// Safe to call multiple times.
func (d *LogExporter) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil
	}
	d.closed = true

	if d.exporter == nil {
		return nil
	}
	return d.exporter.Shutdown(context.Background())
}

// reportError invokes the OnError callback if configured.
func (d *LogExporter) reportError(err error) {
	if d.opts.OnError != nil {
		d.opts.OnError(err)
	}
}

// mapEventToRecord converts a foxcular event to an OTLP log record.
func mapEventToRecord(event *foxcular.Event) sdklog.Record {
	factory := logtest.RecordFactory{
		Timestamp:         event.Timestamp,
		ObservedTimestamp: event.Timestamp,
		Severity:          mapStatusToSeverity(event.Status),
		SeverityText:      mapStatusToSeverityText(event.Status),
		Body:              mapBody(event),
		Attributes:        mapAttributes(event),
	}

	// Map trace/span IDs if present and valid.
	// foxcular IDs may be ULIDs (base32) or other non-hex formats.
	// We deterministically convert them to valid OTEL trace/span IDs.
	if event.TraceID != "" {
		factory.TraceID = idToTraceID(event.TraceID)
	}
	if event.SpanID != "" {
		factory.SpanID = idToSpanID(event.SpanID)
	}

	return factory.NewRecord()
}

// mapStatusToSeverity maps foxcular status to OTLP severity level.
func mapStatusToSeverity(status foxcular.Status) log.Severity {
	switch status {
	case foxcular.StatusOK:
		return log.SeverityInfo
	case foxcular.StatusError:
		return log.SeverityError
	case foxcular.StatusCanceled:
		return log.SeverityWarn
	default:
		return log.SeverityInfo
	}
}

// mapStatusToSeverityText maps foxcular status to a human-readable severity text.
func mapStatusToSeverityText(status foxcular.Status) string {
	switch status {
	case foxcular.StatusOK:
		return "INFO"
	case foxcular.StatusError:
		return "ERROR"
	case foxcular.StatusCanceled:
		return "WARN"
	default:
		return "INFO"
	}
}

// mapBody constructs the OTLP log body from the event.
// The body includes the operation name and message if present.
func mapBody(event *foxcular.Event) log.Value {
	parts := make([]log.Value, 0, 2)
	parts = append(parts, log.StringValue(event.Operation))
	if event.Message != "" {
		parts = append(parts, log.StringValue(event.Message))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return log.StringValue(fmt.Sprintf("%s: %s", event.Operation, event.Message))
}

// mapAttributes converts event fields into OTLP log attributes.
func mapAttributes(event *foxcular.Event) []log.KeyValue {
	attrs := make([]log.KeyValue, 0, 16)

	// Core fields.
	attrs = append(attrs,
		log.String("foxcular.operation", event.Operation),
		log.String("foxcular.status", string(event.Status)),
	)

	if event.Name != "" {
		attrs = append(attrs, log.String("foxcular.name", event.Name))
	}
	if event.Duration > 0 {
		attrs = append(attrs, log.Int64("foxcular.duration_ms", event.Duration.Milliseconds()))
	}
	if event.ErrorMessage != "" {
		attrs = append(attrs, log.String("error.message", event.ErrorMessage))
	}
	if event.ErrorType != "" {
		attrs = append(attrs, log.String("error.type", event.ErrorType))
	}
	if event.ErrorCode != "" {
		attrs = append(attrs, log.String("error.code", event.ErrorCode))
	}
	if event.ParentID != "" {
		attrs = append(attrs, log.String("foxcular.parent_id", event.ParentID))
	}
	if event.Forced {
		attrs = append(attrs, log.Bool("foxcular.forced", true))
	}

	// Map custom data fields as prefixed attributes.
	for k, v := range event.Data {
		if kv, ok := anyToKeyValue("foxcular.data."+k, v); ok {
			attrs = append(attrs, kv)
		}
	}

	return attrs
}

// anyToKeyValue converts a Go value to an OTLP log KeyValue.
func anyToKeyValue(key string, v any) (log.KeyValue, bool) {
	if v == nil {
		return log.Empty(key), true
	}
	switch val := v.(type) {
	case string:
		return log.String(key, val), true
	case bool:
		return log.Bool(key, val), true
	case int:
		return log.Int(key, val), true
	case int64:
		return log.Int64(key, val), true
	case float64:
		return log.Float64(key, val), true
	case time.Duration:
		return log.Int64(key, val.Milliseconds()), true
	case time.Time:
		return log.String(key, val.Format(time.RFC3339Nano)), true
	case map[string]any:
		nested := make([]log.KeyValue, 0, len(val))
		for k, v := range val {
			if kv, ok := anyToKeyValue(k, v); ok {
				nested = append(nested, kv)
			}
		}
		return log.Map(key, nested...), true
	case []any:
		items := make([]log.Value, 0, len(val))
		for _, item := range val {
			if kv, ok := anyToKeyValue("_", item); ok {
				items = append(items, kv.Value)
			}
		}
		return log.Slice(key, items...), true
	case error:
		return log.String(key, val.Error()), true
	default:
		return log.String(key, fmt.Sprintf("%v", val)), true
	}
}

// idToTraceID deterministically converts any string ID to a valid 16-byte
// OTEL TraceID. It first tries hex decoding (for 32-char hex strings), then
// falls back to SHA-256 hashing truncated to 16 bytes.
func idToTraceID(id string) trace.TraceID {
	if len(id) == 32 {
		if tid, err := trace.TraceIDFromHex(id); err == nil {
			return tid
		}
	}
	// Hash the ID to get deterministic 16 bytes.
	h := sha256.Sum256([]byte(id))
	var tid trace.TraceID
	copy(tid[:], h[:16])
	return tid
}

// idToSpanID deterministically converts any string ID to a valid 8-byte
// OTEL SpanID. It first tries hex decoding (for 16-char hex strings), then
// falls back to SHA-256 hashing truncated to 8 bytes.
func idToSpanID(id string) trace.SpanID {
	if len(id) == 16 {
		if sid, err := trace.SpanIDFromHex(id); err == nil {
			return sid
		}
	}
	// Hash the ID to get deterministic 8 bytes.
	h := sha256.Sum256([]byte(id))
	var sid trace.SpanID
	copy(sid[:], h[:8])
	return sid
}
