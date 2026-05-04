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
// via the foxcularBridge drain. When disabled, the original foxctl emit path
// is used directly.

var (
	foxCtlClient   *foxcular.Client
	foxCtlClientMu sync.RWMutex
	foxCtlBridge   *foxcularBridge
	foxCtlEnabled  atomic.Bool
)

// InitFoxcular initializes the foxcular client with a drain that routes events
// back through foxctl's persistence and SSE infrastructure.
// When this is called, Emit/EmitWithConfig will route events through foxcular's
// pipeline (sampling + redaction) before delivery.
// If not called, the original foxctl emit path is used unchanged.
func InitFoxcular() {
	foxCtlClientMu.Lock()
	defer foxCtlClientMu.Unlock()

	if foxCtlBridge != nil {
		return // already initialized
	}

	foxCtlBridge = &foxcularBridge{}

	foxCtlClient = foxcular.NewClient(
		foxCtlBridge,
		foxcular.WithSampler(foxcular.AlwaysSample{}),
		foxcular.WithRedaction(foxcular.NewRedactionPolicy()),
	)

	foxCtlEnabled.Store(true)
}

// CloseFoxcular shuts down the foxcular client.
func CloseFoxcular() error {
	foxCtlClientMu.Lock()
	defer foxCtlClientMu.Unlock()

	if foxCtlClient != nil {
		err := foxCtlClient.Close()
		foxCtlClient = nil
		foxCtlBridge = nil
		foxCtlEnabled.Store(false)
		return err
	}
	return nil
}

// FoxcularClient returns the current foxcular client, or nil if not initialized.
// This is useful for direct foxcular API access when foxctl code wants to use
// foxcular spans or builders directly.
func FoxcularClient() *foxcular.Client {
	foxCtlClientMu.RLock()
	defer foxCtlClientMu.RUnlock()
	return foxCtlClient
}

// emitViaFoxcular routes a WideEvent through the foxcular pipeline.
// Returns true if the event was handled by foxcular, false if foxcular is not enabled.
func emitViaFoxcular(ctx context.Context, event *WideEvent, bypassSampling bool) bool {
	if !foxCtlEnabled.Load() {
		return false
	}

	foxCtlClientMu.RLock()
	client := foxCtlClient
	foxCtlClientMu.RUnlock()

	if client == nil {
		return false
	}

	fe := wideEventToFoxcularEvent(event)
	if bypassSampling {
		fe.Forced = true
	}
	client.EmitEvent(ctx, fe)
	return true
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
