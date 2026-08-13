package semantic

import (
	"context"
	"strings"
	"time"

	"github.com/joshka0/foxsearch/embed"
	"github.com/oklog/ulid/v2"

	"github.com/joshka0/foxctl/internal/platform/observability"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
)

// init wires foxctl-specific host integration into the foxsearch embed module:
// embedding API events are forwarded to the foxcular observability stream, and
// the fallback vector dimension resolves through dbdriver (FOXCTL_VECTOR_DIMS).
func init() {
	embed.SetObserver(emitEmbeddingEvent)
	embed.SetDefaultDimensionsFunc(dbdriver.GetDefaultVectorDimensions)
}

// emitEmbeddingEvent maps a foxsearch embed.Event to a foxcular observability
// event, preserving the historical "embedding.generate" event shape.
func emitEmbeddingEvent(ctx context.Context, e *embed.Event) {
	status := observability.StatusOK
	if e.Status == embed.StatusError {
		status = observability.StatusError
	}

	event := &observability.Event{
		Timestamp: time.Now().UTC(),
		TraceID:   observability.TraceIDFromContext(ctx),
		SpanID:    ulid.Make().String(),
		Operation: "embedding.generate",
		Name:      e.Provider,
		Status:    status,
		Duration:  e.Duration,
		Data: map[string]any{
			observability.DataKeyService:   "foxctl",
			observability.DataKeyComponent: observability.ComponentSkill,
			observability.DataKeySubtype:   e.Model,
			"provider":                     e.Provider,
			"model":                        e.Model,
			"texts_count":                  e.TextsCount,
			"tokens_estimated":             e.TokensEstimated,
			"dimensions":                   e.Dimensions,
			"cost_usd":                     e.CostUSD,
		},
	}
	if e.TokensActual > 0 {
		event.Data["tokens_actual"] = e.TokensActual
	}

	if e.Err != nil {
		event.ErrorMessage = e.Err.Error()
		if status == observability.StatusError {
			// Classify error type
			errMsg := e.Err.Error()
			switch {
			case strings.Contains(errMsg, "rate limit"):
				event.ErrorType = "rate_limit"
			case strings.Contains(errMsg, "timeout"):
				event.ErrorType = "timeout"
			case strings.Contains(errMsg, "API error"):
				event.ErrorType = "api_error"
			default:
				event.ErrorType = "unknown"
			}
		}
	}

	observability.Emit(ctx, event)
}
