package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
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
type ConsoleCancelRuntime struct {
	canceler ConsoleCanceler
	requests chan CancelConsoleSessionRequest
	updates  chan ConsoleCancelUpdate

	ctx    context.Context
	cancel context.CancelFunc

	stopOnce  sync.Once
	waitGroup sync.WaitGroup
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

	ctx, cancel := context.WithCancel(parent)
	runtime := &ConsoleCancelRuntime{
		canceler: canceler,
		requests: make(chan CancelConsoleSessionRequest, requestBufferSize),
		updates:  make(chan ConsoleCancelUpdate, updateBufferSize),
		ctx:      ctx,
		cancel:   cancel,
	}
	runtime.waitGroup.Add(1)
	go runtime.run()
	return runtime, nil
}

// Enqueue trims and adds one cancel request to the bounded runtime queue.
func (runtime *ConsoleCancelRuntime) Enqueue(ctx context.Context, req CancelConsoleSessionRequest) error {
	if runtime == nil {
		return errors.New("console cancel runtime is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sanitized := sanitizeCancelConsoleSessionRequest(req)

	select {
	case <-runtime.ctx.Done():
		return runtime.ctx.Err()
	default:
	}

	select {
	case <-runtime.ctx.Done():
		return runtime.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case runtime.requests <- sanitized:
		return nil
	}
}

// Updates returns the bounded receive-only update channel.
func (runtime *ConsoleCancelRuntime) Updates() <-chan ConsoleCancelUpdate {
	return runtime.updates
}

// Stop cancels processing and waits for the worker goroutine to exit.
func (runtime *ConsoleCancelRuntime) Stop() {
	if runtime == nil {
		return
	}
	runtime.stopOnce.Do(func() {
		runtime.cancel()
		runtime.waitGroup.Wait()
	})
}

// Close is an alias for Stop.
func (runtime *ConsoleCancelRuntime) Close() {
	runtime.Stop()
}

func (runtime *ConsoleCancelRuntime) run() {
	defer runtime.waitGroup.Done()
	defer close(runtime.updates)

	for {
		select {
		case <-runtime.ctx.Done():
			return
		case req := <-runtime.requests:
			response, err := runtime.canceler.SubmitCancel(runtime.ctx, req)
			if err != nil {
				if sendErr := runtime.sendUpdate(ConsoleCancelUpdate{
					Type: ConsoleCancelUpdateError,
					Failed: &ConsoleCancelFailed{
						CorrelationID: req.CorrelationID,
						Err:           err,
					},
				}); sendErr != nil {
					return
				}
				continue
			}

			if !response.OK {
				message := strings.TrimSpace(response.Message)
				if message == "" {
					message = "cancel request was not accepted"
				}
				if sendErr := runtime.sendUpdate(ConsoleCancelUpdate{
					Type: ConsoleCancelUpdateError,
					Failed: &ConsoleCancelFailed{
						CorrelationID: req.CorrelationID,
						Err:           errors.New(message),
					},
				}); sendErr != nil {
					return
				}
				continue
			}

			if sendErr := runtime.sendUpdate(ConsoleCancelUpdate{
				Type: ConsoleCancelUpdateAccepted,
				Accepted: &ConsoleCancelAccepted{
					CorrelationID: req.CorrelationID,
					Message:       response.Message,
				},
			}); sendErr != nil {
				return
			}
		}
	}
}

func (runtime *ConsoleCancelRuntime) sendUpdate(update ConsoleCancelUpdate) error {
	select {
	case <-runtime.ctx.Done():
		return runtime.ctx.Err()
	case runtime.updates <- update:
		return nil
	}
}

func sanitizeCancelConsoleSessionRequest(req CancelConsoleSessionRequest) CancelConsoleSessionRequest {
	return CancelConsoleSessionRequest{
		CorrelationID: strings.TrimSpace(req.CorrelationID),
	}
}
