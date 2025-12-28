package observability

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog"
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
	if event == nil {
		return
	}

	// Check if observability is enabled
	dir := getObsDir()
	if dir == "" {
		return
	}

	// Apply sampling
	sampler := getSampler()
	if sampler != nil {
		decision := sampler.ShouldSample(event)
		if decision == Drop {
			return
		}
	}

	// Write the event
	if err := WriteEvent(ctx, WideEventFileName, event); err != nil {
		logEmitError("emit", event.Operation, err)
	}
}

// EmitSync writes a WideEvent synchronously, bypassing sampling.
// Use this for critical events that must always be recorded.
func EmitSync(ctx context.Context, event *WideEvent) error {
	if event == nil {
		return nil
	}

	dir := getObsDir()
	if dir == "" {
		return nil
	}

	return WriteEvent(ctx, WideEventFileName, event)
}

// EmitBuilder is a convenience function that builds and emits an event.
// It applies sampling based on the configured sampler.
func EmitBuilder(ctx context.Context, builder *EventBuilder, status Status, duration int64) {
	if builder == nil {
		return
	}

	event := builder.Build()
	event.Status = status
	event.DurationMS = duration

	Emit(ctx, event)
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

func logEmitError(op, operation string, err error) {
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	log.Warn().
		Str("component", "observability").
		Str("op", op).
		Str("operation", operation).
		Err(err).
		Msg("wide event emit failed")
}
