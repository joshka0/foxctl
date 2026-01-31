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

// EmitWithConfig writes a WideEvent with custom persistence configuration.
// EmitWithConfig emits a WideEvent by streaming it to SSE and optionally persisting it to disk.
// 
// EmitWithConfig does nothing if event is nil. It always publishes the event to SSE for real‑time
// streaming. If no observability directory is configured, persistence is skipped. When persistence is
// enabled, the active sampler (if any) is consulted and a Drop decision prevents file persistence.
// If config is nil, the event is persisted using the default NDJSON format.
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
// EmitSync emits a WideEvent immediately. It always publishes the event to the server-sent events (SSE) stream and, if file-based observability is enabled, appends the event to the wide_events NDJSON file.
// EmitSync bypasses any sampling; calling it with a nil event is a no-op. It returns any error encountered while writing the event to persistent storage.
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

// EmitBuilder is a convenience function that builds and emits an event.
// It applies sampling based on the configured sampler.
// Persist config from the builder (via WithPersistence/WithPersistenceFile) is honored.
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
// If sampler is nil, DefaultSampler() is used.
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