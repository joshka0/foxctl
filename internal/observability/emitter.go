package observability

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// WideEventFileName is the NDJSON file for wide events.
const WideEventFileName = "wide_events"

var (
	emitterSampler     Sampler = nil
	emitterSamplerOnce sync.Once
)

// SetSamplerForTesting overrides the sampler for testing purposes.
// This should only be called from tests.
func SetSamplerForTesting(s Sampler) {
	emitterSampler = s
}

func getSampler() Sampler {
	emitterSamplerOnce.Do(func() {
		if emitterSampler == nil {
			emitterSampler = DefaultSampler()
		}
	})
	return emitterSampler
}

// Emit writes a WideEvent to the observability stream if:
// 1. AGENTCTL_OBS_DIR is configured
// 2. The sampler decides to sample the event
//
// This function is safe to call from any goroutine.
// Errors are logged but not returned - observability is best-effort.
func Emit(ctx context.Context, event *WideEvent) {
	EmitWithConfig(ctx, event, nil)
}

// EmitWithConfig emits a WideEvent with custom persistence configuration.
//
// Index:
// - Purpose: Stream an event to SSE and optionally persist it to disk
// - Flow: publish to SSE → resolve observability dir → apply sampling → persist event
// - SideEffects: writes SSE output; optional NDJSON file writes
// - FailureModes: persistence failures are logged; nil event returns early
// - Observability: emits WideEvent to SSE and wide_events NDJSON persistence
// - Related: Emit, EmitSync, persistEvent, publishToSSE
// - Keywords: wide_events, observability_dir, sampler, sse, persist_event
func EmitWithConfig(ctx context.Context, event *WideEvent, config *persistConfig) {
	if event == nil {
		return
	}

	// Always try to publish to SSE (even if file persistence is disabled)
	// This allows real-time activity streaming without requiring AGENTCTL_OBS_DIR
	publishToSSE(event)

	// Check if file observability is enabled
	dir := getObsDir()
	if dir == "" {
		return
	}

	// Apply sampling for file persistence
	sampler := getSampler()
	if sampler != nil {
		decision := sampler.ShouldSample(event)
		if decision == Drop {
			return
		}
	}

	// Use custom persistence if configured, otherwise default NDJSON
	persistEvent(ctx, event, config)
}

// EmitSync writes a WideEvent synchronously, bypassing sampling.
//
// Index:
// - Purpose: Persist a WideEvent immediately without sampling
// - Flow: publish to SSE → resolve observability dir → append NDJSON
// - SideEffects: writes SSE output; appends to wide_events NDJSON
// - FailureModes: file write errors returned; nil event returns nil
// - Observability: emits WideEvent to SSE and wide_events NDJSON persistence
// - Related: EmitWithConfig, WriteEvent, publishToSSE
// - Keywords: wide_events, emit_sync, sse, write_event, observability_dir
func EmitSync(ctx context.Context, event *WideEvent) error {
	if event == nil {
		return nil
	}

	// Always try to publish to SSE (even if file persistence is disabled)
	publishToSSE(event)

	dir := getObsDir()
	if dir == "" {
		return nil
	}

	return WriteEvent(ctx, WideEventFileName, event)
}

// EmitBuilder builds and emits an event using builder persistence settings.
//
// Index:
// - Purpose: Build a WideEvent and emit it with builder-specified persistence
// - Flow: build event → set status/duration → delegate to EmitWithConfig
// - SideEffects: publishes SSE; optional NDJSON persistence
// - FailureModes: nil builder returns early; persistence failures logged downstream
// - Observability: emits WideEvent to SSE and optional persistence
// - Related: EventBuilder.Build, EmitWithConfig
// - Keywords: event_builder, emit, status, duration_ms, persist_config
func EmitBuilder(ctx context.Context, builder *EventBuilder, status Status, duration int64) {
	if builder == nil {
		return
	}

	event := builder.Build()
	event.Status = status
	event.DurationMS = duration

	// Pass builder's persist config to honor WithPersistence/WithPersistenceFile
	EmitWithConfig(ctx, event, builder.PersistConfig())
}

// WideEventWriter provides a structured way to write wide events.
// It caches the file handle for better performance when writing many events.
type WideEventWriter struct {
	mu       sync.Mutex
	file     *os.File
	encoder  *json.Encoder
	sampler  Sampler
	filePath string
}

// NewWideEventWriter creates a new writer for wide events.
//
// Index:
// - Purpose: Initialize a cached NDJSON writer for WideEvents
// - Flow: resolve observability dir → ensure events dir → open file → select sampler
// - SideEffects: creates directories; opens file handles
// - FailureModes: directory creation errors, file open errors
// - Related: WideEventWriter.Write, DefaultSampler
// - Keywords: wide_events, ndjson, sampler, observability_dir, file_handle
func NewWideEventWriter(sampler Sampler) (*WideEventWriter, error) {
	dir := getObsDir()
	if dir == "" {
		return nil, nil // Observability disabled
	}

	eventsDir := filepath.Join(dir, "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		return nil, err
	}

	filePath := filepath.Join(eventsDir, WideEventFileName+".ndjson")
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	if sampler == nil {
		sampler = DefaultSampler()
	}

	return &WideEventWriter{
		file:     f,
		encoder:  json.NewEncoder(f),
		sampler:  sampler,
		filePath: filePath,
	}, nil
}

// Write writes an event if the sampler allows it.
func (w *WideEventWriter) Write(event *WideEvent) error {
	if w == nil || event == nil {
		return nil
	}

	if w.sampler != nil {
		if decision := w.sampler.ShouldSample(event); decision == Drop {
			return nil
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	return w.encoder.Encode(event)
}

// WriteAlways writes an event bypassing the sampler.
func (w *WideEventWriter) WriteAlways(event *WideEvent) error {
	if w == nil || event == nil {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	return w.encoder.Encode(event)
}

// Close closes the underlying file handle.
func (w *WideEventWriter) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	return w.file.Close()
}
