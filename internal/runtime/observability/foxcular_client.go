package observability

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/joshka0/foxcular"
)

// Foxcular integration state.
// The foxcular client is optional: when enabled, events flow through foxcular's
// client pipeline (sampling, redaction) before reaching foxctl's persistence
// and SSE infrastructure. When disabled, the original foxctl emit path is used.
//
// Architecture: foxcular handles cross-cutting concerns (redaction, sampling)
// and foxctl retains control of its persistence config (NDJSON/SQLite/hybrid,
// custom filenames). This is achieved by using foxcular's prepare logic
// (sampling + redaction) via the foxcular client, then calling foxctl's own
// persistence and SSE publishing path with the redacted/sampled event and
// the caller's persistConfig.

var (
	foxCtlClient   *foxcular.Client
	foxCtlClientMu sync.RWMutex
	foxCtlEnabled  atomic.Bool
	foxCtlRedactor *foxcular.RedactionPolicy
	foxCtlSampler  foxcular.Sampler
)

// InitFoxcular initializes the foxcular integration for foxctl's emit pipeline.
// When this is called, Emit/EmitWithConfig will route events through foxcular's
// redaction and sampling before delivery.
// If not called, the original foxctl emit path is used unchanged.
func InitFoxcular() {
	foxCtlClientMu.Lock()
	defer foxCtlClientMu.Unlock()

	if foxCtlEnabled.Load() {
		return // already initialized
	}

	foxCtlRedactor = foxcular.NewRedactionPolicy()
	foxCtlSampler = foxcular.AlwaysSample{} // foxctl applies its own sampling downstream
	foxCtlEnabled.Store(true)
}

// CloseFoxcular shuts down the foxcular integration.
func CloseFoxcular() error {
	foxCtlClientMu.Lock()
	defer foxCtlClientMu.Unlock()

	foxCtlClient = nil
	foxCtlRedactor = nil
	foxCtlSampler = nil
	foxCtlEnabled.Store(false)
	return nil
}

// ResetFoxcularForTesting resets the foxcular state for test isolation.
func ResetFoxcularForTesting() {
	foxCtlClientMu.Lock()
	defer foxCtlClientMu.Unlock()
	if foxCtlClient != nil {
		_ = foxCtlClient.Close()
	}
	foxCtlClient = nil
	foxCtlRedactor = nil
	foxCtlSampler = nil
	foxCtlEnabled.Store(false)
}

// FoxcularClient returns the current foxcular client, or nil if not initialized.
// This is useful for direct foxcular API access when foxctl code wants to use
// foxcular spans or builders directly.
func FoxcularClient() *foxcular.Client {
	foxCtlClientMu.RLock()
	defer foxCtlClientMu.RUnlock()
	return foxCtlClient
}

// foxcularRedactAndSample applies foxcular's redaction and sampling to a WideEvent.
// It converts the WideEvent to a foxcular Event, applies redaction and sampling,
// then converts back. Returns the (possibly modified) WideEvent and true if the
// event should be kept, or nil and false if the event was dropped by sampling.
// The original event is not modified; a new event is returned if redaction changed it.
func foxcularRedactAndSample(event *WideEvent, bypassSampling bool) (*WideEvent, bool) {
	// Convert to foxcular event for redaction + sampling
	fe := wideEventToFoxcularEvent(event)
	if bypassSampling {
		fe.Forced = true
	}

	// Apply foxcular redaction
	if foxCtlRedactor != nil && foxCtlRedactor.Enabled() {
		fe = foxCtlRedactor.RedactEvent(fe)
	}

	// Apply foxcular sampling (unless forced/bypassed)
	if !fe.Forced && foxCtlSampler != nil {
		if foxCtlSampler.ShouldSample(fe) == foxcular.Drop {
			return nil, false
		}
	}

	// Convert back to foxctl WideEvent with redacted data
	we := foxcularEventToWideEvent(fe)

	// Preserve the original Data map entries that were not in foxctl-specific fields
	// to avoid losing custom data during the round-trip.
	// The round-trip through foxcular preserves data keys in Data that are not
	// foxctl-specific structured fields.
	return we, true
}

// emitViaFoxcular routes a WideEvent through the foxcular pipeline.
// Returns (redactedEvent, true) if the event was handled by foxcular,
// (nil, false) if foxcular is not enabled.
// The caller is responsible for SSE publishing and persistence using the
// returned redacted event and the original persistConfig.
func emitViaFoxcular(event *WideEvent, bypassSampling bool) (*WideEvent, bool) {
	if !foxCtlEnabled.Load() {
		return nil, false
	}

	foxCtlClientMu.RLock()
	defer foxCtlClientMu.RUnlock()

	if foxCtlRedactor == nil {
		return nil, false
	}

	we, ok := foxcularRedactAndSample(event, bypassSampling)
	if !ok {
		// Event was dropped by sampling - still return true to indicate foxcular
		// handled the decision (don't fall through to legacy path).
		return nil, true
	}

	return we, true
}

// StartFoxcularSpan creates a foxcular span that delivers through foxctl's persistence.
// Returns the derived context and the foxcular span.
// Returns nil if foxcular is not initialized.
func StartFoxcularSpan(ctx context.Context, operation string, opts ...foxcular.SpanOption) (context.Context, *foxcular.Span) {
	foxCtlClientMu.RLock()
	client := foxCtlClient
	foxCtlClientMu.RUnlock()

	if client == nil {
		return ctx, nil
	}

	return client.StartSpan(ctx, operation, opts...)
}
