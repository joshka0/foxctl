package embed

import (
	"context"
	"sync/atomic"
	"time"
)

// Status classifies the outcome of an embedding API call.
type Status string

const (
	// StatusOK indicates the call completed successfully.
	StatusOK Status = "ok"
	// StatusError indicates the call failed.
	StatusError Status = "error"
)

// Event describes a single embedding API call outcome. It is emitted to the
// registered Observer after every provider request so hosts can wire their own
// telemetry without foxsearch depending on a specific observability stack.
type Event struct {
	// Provider is the provider name (e.g. "openai_compat", "gemini").
	Provider string
	// Model is the model identifier used for the call.
	Model string
	// TextsCount is the number of texts in the request.
	TextsCount int
	// TokensEstimated is the estimated input token count (~4 chars/token).
	TokensEstimated int64
	// TokensActual is the actual token count when the API reports it.
	TokensActual int64
	// Dimensions is the embedding dimension of the model.
	Dimensions int
	// CostUSD is the estimated cost of the call in USD.
	CostUSD float64
	// Duration is the wall-clock time of the API call.
	Duration time.Duration
	// Status is StatusOK or StatusError.
	Status Status
	// Err is the call error, when Status is StatusError.
	Err error
}

// Observer receives an Event after each provider API call.
type Observer func(ctx context.Context, e *Event)

var observer atomic.Pointer[Observer]

// SetObserver installs the process-wide observer for embedding API events.
// Passing nil disables event emission. It is safe to call during package init.
func SetObserver(o Observer) {
	if o == nil {
		observer.Store(nil)
		return
	}
	observer.Store(&o)
}

func emitObserverEvent(ctx context.Context, e *Event) {
	if o := observer.Load(); o != nil {
		(*o)(ctx, e)
	}
}
