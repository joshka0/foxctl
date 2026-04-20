package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/joshka0/foxctl/internal/interfaces/tui/runtime"
)

const (
	defaultConsoleCancelRequestBufferSize = 16
	defaultConsoleCancelUpdateBufferSize  = 16
)

// ConsoleCanceler submits queued console cancel requests.
type ConsoleCanceler interface {
	SubmitCancel(ctx context.Context, req CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error)
}

// ConsoleCancelerFunc adapts a function into a ConsoleCanceler.
type ConsoleCancelerFunc func(ctx context.Context, req CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error)

// SubmitCancel implements ConsoleCanceler.
func (fn ConsoleCancelerFunc) SubmitCancel(ctx context.Context, req CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error) {
	if fn == nil {
		return CancelConsoleSessionResponse{}, errors.New("console canceler function is required")
	}
	return fn(ctx, req)
}

// NewHTTPConsoleCanceler wraps ConsoleAdapter.CancelSession for one existing session id.
func NewHTTPConsoleCanceler(adapter *ConsoleAdapter, sessionID string) (ConsoleCanceler, error) {
	if adapter == nil {
		return nil, errors.New("console adapter is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}

	return ConsoleCancelerFunc(func(ctx context.Context, req CancelConsoleSessionRequest) (CancelConsoleSessionResponse, error) {
		return adapter.CancelSession(ctx, sessionID, req)
	}), nil
}

// ConsoleCancelUpdateType identifies the outcome of a queued cancel request.
type ConsoleCancelUpdateType string

const (
	ConsoleCancelUpdateAccepted ConsoleCancelUpdateType = "accepted"
	ConsoleCancelUpdateError    ConsoleCancelUpdateType = "error"
)

type ConsoleCancelAccepted struct {
	CorrelationID string
	Message       string
}

type ConsoleCancelFailed struct {
	CorrelationID string
	Err           error
}

// ConsoleCancelUpdate is a typed result from the cancel runtime.
type ConsoleCancelUpdate struct {
	Type     ConsoleCancelUpdateType
	Accepted *ConsoleCancelAccepted
	Failed   *ConsoleCancelFailed
}

// ConsoleCancelRuntime owns one goroutine that submits queued cancel requests.
// It delegates goroutine lifecycle to runtime.Bounded.
type ConsoleCancelRuntime struct {
	bounded *runtime.Bounded[CancelConsoleSessionRequest, ConsoleCancelUpdate]
}

// NewConsoleCancelRuntime creates and starts a bounded cancel runtime.
func NewConsoleCancelRuntime(
	parent context.Context,
	canceler ConsoleCanceler,
	requestBufferSize int,
	updateBufferSize int,
) (*ConsoleCancelRuntime, error) {
	if canceler == nil {
		return nil, errors.New("console canceler is required")
	}
	if parent == nil {
		parent = context.Background()
	}

	if requestBufferSize < 0 {
		return nil, errors.New("console cancel request buffer size must be >= 0")
	}
	if requestBufferSize == 0 {
		requestBufferSize = defaultConsoleCancelRequestBufferSize
	}

	if updateBufferSize < 0 {
		return nil, errors.New("console cancel update buffer size must be >= 0")
	}
	if updateBufferSize == 0 {
		updateBufferSize = defaultConsoleCancelUpdateBufferSize
	}

	handler := func(ctx context.Context, req CancelConsoleSessionRequest) ConsoleCancelUpdate {
		response, err := canceler.SubmitCancel(ctx, req)
		if err != nil {
			return ConsoleCancelUpdate{
				Type: ConsoleCancelUpdateError,
				Failed: &ConsoleCancelFailed{
					CorrelationID: req.CorrelationID,
					Err:           err,
				},
			}
		}

		if !response.OK {
			message := strings.TrimSpace(response.Message)
			if message == "" {
				message = "cancel request was not accepted"
			}
			return ConsoleCancelUpdate{
				Type: ConsoleCancelUpdateError,
				Failed: &ConsoleCancelFailed{
					CorrelationID: req.CorrelationID,
					Err:           errors.New(message),
				},
			}
		}

		return ConsoleCancelUpdate{
			Type: ConsoleCancelUpdateAccepted,
			Accepted: &ConsoleCancelAccepted{
				CorrelationID: req.CorrelationID,
				Message:       response.Message,
			},
		}
	}

	b, err := runtime.NewBounded(parent, requestBufferSize, updateBufferSize, handler)
	if err != nil {
		return nil, err
	}
	return &ConsoleCancelRuntime{bounded: b}, nil
}

// Enqueue trims and adds one cancel request to the bounded runtime queue.
func (rt *ConsoleCancelRuntime) Enqueue(ctx context.Context, req CancelConsoleSessionRequest) error {
	if rt == nil {
		return errors.New("console cancel runtime is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sanitized := sanitizeCancelConsoleSessionRequest(req)

	err := rt.bounded.Enqueue(ctx, sanitized)
	if err != nil {
		// Map ErrStopped to context.Canceled to preserve existing behavior.
		if errors.Is(err, runtime.ErrStopped) {
			return rt.bounded.Context().Err()
		}
		return err
	}
	return nil
}

// Updates returns the bounded receive-only update channel.
func (rt *ConsoleCancelRuntime) Updates() <-chan ConsoleCancelUpdate {
	return rt.bounded.Updates()
}

// Stop cancels processing and waits for the worker goroutine to exit.
func (rt *ConsoleCancelRuntime) Stop() {
	if rt == nil {
		return
	}
	rt.bounded.Stop()
}

// Close is an alias for Stop.
func (rt *ConsoleCancelRuntime) Close() {
	rt.Stop()
}

func sanitizeCancelConsoleSessionRequest(req CancelConsoleSessionRequest) CancelConsoleSessionRequest {
	return CancelConsoleSessionRequest{
		CorrelationID: strings.TrimSpace(req.CorrelationID),
	}
}
