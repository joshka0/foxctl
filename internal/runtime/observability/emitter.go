package observability

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/joshka0/foxcular"
)

// EventFileName is the NDJSON file for foxcular events.
const EventFileName = "foxcular_events"

var (
	emitterSampler   Sampler
	emitterSamplerMu sync.RWMutex
	emitterClient    = foxcular.NewClient(foxcularDrain{}, foxcular.WithSampler(foxcular.AlwaysSample{}))
)

// SetSamplerForTesting overrides the sampler for testing purposes.
// This should only be called from tests.
func SetSamplerForTesting(s Sampler) {
	emitterSamplerMu.Lock()
	defer emitterSamplerMu.Unlock()
	emitterSampler = s
}

func getSampler() Sampler {
	emitterSamplerMu.RLock()
	sampler := emitterSampler
	emitterSamplerMu.RUnlock()
	if sampler != nil {
		return sampler
	}
	return DefaultSampler()
}

// Emit writes a Event to the observability stream if:
// 1. FOXCTL_OBS_DIR is configured
// 2. The sampler decides to sample the event
//
// This function is safe to call from any goroutine.
// Errors are logged but not returned - observability is best-effort.
func Emit(ctx context.Context, event *Event) {
	EmitWithConfig(ctx, event, nil)
}

// EmitWithConfig emits a Event with custom persistence configuration.
//
// Index:
//
//	Purpose: Stream an event to SSE and optionally persist it to disk
//	Flow: publish to SSE → resolve observability dir → apply sampling → persist event
//	Related: Emit, EmitSync, persistEvent, publishToSSE
//	Keywords: foxcular_events, observability_dir, sampler, sse, persist_event
//
// [[protocol:event-emit-with-config]]
// [[domain:observability-event-streaming]]
func EmitWithConfig(ctx context.Context, event *Event, config *persistConfig) {
	if event == nil {
		return
	}
	ctx = context.WithValue(ctx, foxcularPersistConfigKey{}, foxcularPersistOptions{config: config})
	emitterClient.EmitEvent(ctx, event)
}

// EmitSync writes a Event synchronously, bypassing sampling.
//
// Index:
//
//	Purpose: Persist a Event immediately without sampling
//	Flow: publish to SSE → resolve observability dir → append NDJSON
//	Related: EmitWithConfig, WriteEvent, publishToSSE
//	Keywords: foxcular_events, emit_sync, sse, write_event, observability_dir
//
// [[protocol:event-emit-sync]]
// [[domain:observability-synchronous-persistence]]
func EmitSync(ctx context.Context, event *Event) error {
	if event == nil {
		return nil
	}
	ctx = context.WithValue(ctx, foxcularPersistConfigKey{}, foxcularPersistOptions{syncWrite: true})
	return emitterClient.EmitEventSync(ctx, event)
}

// EmitBuilder builds and emits an event using builder persistence settings.
//
// Index:
//
//	Purpose: Build a Event and emit it with builder-specified persistence
//	Flow: build event → set status/duration → delegate to EmitWithConfig
//	Related: EventBuilder.Build, EmitWithConfig
//	Keywords: event_builder, emit, status, duration_ms, persist_config
//
// [[protocol:event-builder-emit]]
// [[domain:observability-builder-pattern]]
func EmitBuilder(ctx context.Context, builder *EventBuilder, status Status, duration int64) {
	if builder == nil {
		return
	}

	event := builder.Build()
	event.Status = status
	event.Duration = time.Duration(duration) * time.Millisecond

	// Pass builder's persist config to honor WithPersistence/WithPersistenceFile
	EmitWithConfig(ctx, event, builder.PersistConfig())
}

// EventWriter provides a structured way to write canonical events.
// It caches the file handle for better performance when writing many events.
type EventWriter struct {
	mu       sync.Mutex
	file     *os.File
	encoder  *json.Encoder
	sampler  Sampler
	filePath string
}

// NewEventWriter creates a new writer for events.
//
// Index:
//
//	Purpose: Initialize a cached NDJSON writer for events
//	Flow: resolve observability dir → ensure events dir → open file → select sampler
//	Related: EventWriter.Write, DefaultSampler
//	Keywords: foxcular_events, ndjson, sampler, observability_dir, file_handle
//
// [[domain:observability-event-writer]]
func NewEventWriter(sampler Sampler) (*EventWriter, error) {
	dir := getObsDir()
	if dir == "" {
		return nil, nil // Observability disabled
	}

	eventsDir := filepath.Join(dir, "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		return nil, err
	}

	filePath := filepath.Join(eventsDir, EventFileName+".ndjson")
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	if sampler == nil {
		sampler = DefaultSampler()
	}

	return &EventWriter{
		file:     f,
		encoder:  json.NewEncoder(f),
		sampler:  sampler,
		filePath: filePath,
	}, nil
}

// Write writes an event if the sampler allows it.
// Sensitive fields are redacted before encoding.
func (w *EventWriter) Write(event *Event) error {
	if w == nil || event == nil {
		return nil
	}

	if w.sampler != nil {
		if decision := w.sampler.ShouldSample(event); decision == Drop {
			return nil
		}
	}

	event = foxcular.NewRedactionPolicy().RedactEvent(event)

	w.mu.Lock()
	defer w.mu.Unlock()

	return w.encoder.Encode(event)
}

// WriteAlways writes an event bypassing the sampler.
// Sensitive fields are redacted before encoding.
func (w *EventWriter) WriteAlways(event *Event) error {
	if w == nil || event == nil {
		return nil
	}

	event = foxcular.NewRedactionPolicy().RedactEvent(event)

	w.mu.Lock()
	defer w.mu.Unlock()

	return w.encoder.Encode(event)
}

type foxcularPersistConfigKey struct{}

type foxcularPersistOptions struct {
	config    *persistConfig
	syncWrite bool
}

type foxcularDrain struct{}

func (foxcularDrain) Send(ctx context.Context, event *foxcular.Event) error {
	if event == nil {
		return nil
	}
	publishToSSE(event)
	if getObsDir() == "" {
		return nil
	}
	opts, _ := ctx.Value(foxcularPersistConfigKey{}).(foxcularPersistOptions)
	if opts.syncWrite {
		fileName := EventFileName
		if opts.config != nil && opts.config.fileName != "" {
			fileName = opts.config.fileName
		}
		return WriteEvent(ctx, fileName, event)
	}
	sampler := getSampler()
	if sampler != nil && sampler.ShouldSample(event) == Drop {
		return nil
	}
	persistEvent(ctx, event, opts.config)
	return nil
}

func (foxcularDrain) Flush(context.Context) error {
	return nil
}

func (foxcularDrain) Close() error {
	return nil
}

// Close closes the underlying file handle.
func (w *EventWriter) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	return w.file.Close()
}
