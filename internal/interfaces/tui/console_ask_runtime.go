package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
)

const (
	defaultConsoleAskRequestBufferSize = 16
	defaultConsoleAskUpdateBufferSize  = 16
)

// ConsoleAskSubmitter submits queued console ask requests.
type ConsoleAskSubmitter interface {
	SubmitAsk(ctx context.Context, req AskConsoleSessionRequest) (AskConsoleSessionResponse, error)
}

// ConsoleAskSubmitterFunc adapts a function into a ConsoleAskSubmitter.
type ConsoleAskSubmitterFunc func(ctx context.Context, req AskConsoleSessionRequest) (AskConsoleSessionResponse, error)

// SubmitAsk implements ConsoleAskSubmitter.
func (fn ConsoleAskSubmitterFunc) SubmitAsk(ctx context.Context, req AskConsoleSessionRequest) (AskConsoleSessionResponse, error) {
	if fn == nil {
		return AskConsoleSessionResponse{}, errors.New("console ask submitter function is required")
	}
	return fn(ctx, req)
}

// NewHTTPConsoleAskSubmitter wraps ConsoleAdapter.AskSession for one existing session id.
func NewHTTPConsoleAskSubmitter(adapter *ConsoleAdapter, sessionID string) (ConsoleAskSubmitter, error) {
	if adapter == nil {
		return nil, errors.New("console adapter is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}

	return ConsoleAskSubmitterFunc(func(ctx context.Context, req AskConsoleSessionRequest) (AskConsoleSessionResponse, error) {
		return adapter.AskSession(ctx, sessionID, req)
	}), nil
}

// ConsoleAskUpdateType identifies the outcome of a queued ask request.
type ConsoleAskUpdateType string

const (
	ConsoleAskUpdateAccepted ConsoleAskUpdateType = "accepted"
	ConsoleAskUpdateError    ConsoleAskUpdateType = "error"
)

type ConsoleAskAccepted struct {
	Content       string
	CorrelationID string
	Message       string
}

type ConsoleAskFailed struct {
	Content       string
	CorrelationID string
	Err           error
}

// ConsoleAskUpdate is a typed result from the ask runtime.
type ConsoleAskUpdate struct {
	Type     ConsoleAskUpdateType
	Accepted *ConsoleAskAccepted
	Failed   *ConsoleAskFailed
}

// ConsoleAskRuntime owns one goroutine that submits queued asks.
type ConsoleAskRuntime struct {
	submitter ConsoleAskSubmitter
	requests  chan AskConsoleSessionRequest
	updates   chan ConsoleAskUpdate

	ctx    context.Context
	cancel context.CancelFunc

	stopOnce  sync.Once
	waitGroup sync.WaitGroup
}

// NewConsoleAskRuntime creates and starts a bounded ask runtime.
func NewConsoleAskRuntime(
	parent context.Context,
	submitter ConsoleAskSubmitter,
	requestBufferSize int,
	updateBufferSize int,
) (*ConsoleAskRuntime, error) {
	if submitter == nil {
		return nil, errors.New("console ask submitter is required")
	}
	if parent == nil {
		parent = context.Background()
	}

	if requestBufferSize < 0 {
		return nil, errors.New("console ask request buffer size must be >= 0")
	}
	if requestBufferSize == 0 {
		requestBufferSize = defaultConsoleAskRequestBufferSize
	}

	if updateBufferSize < 0 {
		return nil, errors.New("console ask update buffer size must be >= 0")
	}
	if updateBufferSize == 0 {
		updateBufferSize = defaultConsoleAskUpdateBufferSize
	}

	ctx, cancel := context.WithCancel(parent)
	runtime := &ConsoleAskRuntime{
		submitter: submitter,
		requests:  make(chan AskConsoleSessionRequest, requestBufferSize),
		updates:   make(chan ConsoleAskUpdate, updateBufferSize),
		ctx:       ctx,
		cancel:    cancel,
	}
	runtime.waitGroup.Add(1)
	go runtime.run()
	return runtime, nil
}

// Enqueue validates and adds one ask request to the bounded runtime queue.
func (runtime *ConsoleAskRuntime) Enqueue(ctx context.Context, req AskConsoleSessionRequest) error {
	if runtime == nil {
		return errors.New("console ask runtime is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sanitized, err := sanitizeAskConsoleSessionRequest(req)
	if err != nil {
		return err
	}

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
func (runtime *ConsoleAskRuntime) Updates() <-chan ConsoleAskUpdate {
	return runtime.updates
}

// Stop cancels processing and waits for the worker goroutine to exit.
func (runtime *ConsoleAskRuntime) Stop() {
	if runtime == nil {
		return
	}
	runtime.stopOnce.Do(func() {
		runtime.cancel()
		runtime.waitGroup.Wait()
	})
}

// Close is an alias for Stop.
func (runtime *ConsoleAskRuntime) Close() {
	runtime.Stop()
}

func (runtime *ConsoleAskRuntime) run() {
	defer runtime.waitGroup.Done()
	defer close(runtime.updates)

	for {
		select {
		case <-runtime.ctx.Done():
			return
		case req := <-runtime.requests:
			response, err := runtime.submitter.SubmitAsk(runtime.ctx, req)
			if err != nil {
				if sendErr := runtime.sendUpdate(ConsoleAskUpdate{
					Type: ConsoleAskUpdateError,
					Failed: &ConsoleAskFailed{
						Content:       req.Content,
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
					message = "ask request was not accepted"
				}
				if sendErr := runtime.sendUpdate(ConsoleAskUpdate{
					Type: ConsoleAskUpdateError,
					Failed: &ConsoleAskFailed{
						Content:       req.Content,
						CorrelationID: firstNonEmpty(response.CorrelationID, req.CorrelationID),
						Err:           errors.New(message),
					},
				}); sendErr != nil {
					return
				}
				continue
			}

			if sendErr := runtime.sendUpdate(ConsoleAskUpdate{
				Type: ConsoleAskUpdateAccepted,
				Accepted: &ConsoleAskAccepted{
					Content:       req.Content,
					CorrelationID: firstNonEmpty(response.CorrelationID, req.CorrelationID),
					Message:       response.Message,
				},
			}); sendErr != nil {
				return
			}
		}
	}
}

func (runtime *ConsoleAskRuntime) sendUpdate(update ConsoleAskUpdate) error {
	select {
	case <-runtime.ctx.Done():
		return runtime.ctx.Err()
	case runtime.updates <- update:
		return nil
	}
}

func sanitizeAskConsoleSessionRequest(req AskConsoleSessionRequest) (AskConsoleSessionRequest, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return AskConsoleSessionRequest{}, errors.New("ask content is required")
	}

	return AskConsoleSessionRequest{
		Content:       content,
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		LLMProvider:   strings.TrimSpace(req.LLMProvider),
		LLMModel:      strings.TrimSpace(req.LLMModel),
	}, nil
}
