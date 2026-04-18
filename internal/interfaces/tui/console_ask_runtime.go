package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/joshka0/foxctl/internal/interfaces/tui/runtime"
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

// NewHTTPAgentAskSubmitter wraps AgentAdapter.AskAgent for one companion agent.
func NewHTTPAgentAskSubmitter(adapter *AgentAdapter, agentID string) (ConsoleAskSubmitter, error) {
	if adapter == nil {
		return nil, errors.New("agent adapter is required")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, errors.New("agent id is required")
	}

	return ConsoleAskSubmitterFunc(func(ctx context.Context, req AskConsoleSessionRequest) (AskConsoleSessionResponse, error) {
		response, err := adapter.AskAgent(ctx, agentID, AskAgentRequest{Message: req.Content})
		if err != nil {
			return AskConsoleSessionResponse{}, err
		}
		return AskConsoleSessionResponse{
			OK:      true,
			Message: strings.TrimSpace(response.Reply),
		}, nil
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
// It delegates goroutine lifecycle to runtime.Bounded.
type ConsoleAskRuntime struct {
	bounded *runtime.Bounded[AskConsoleSessionRequest, ConsoleAskUpdate]
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

	handler := func(ctx context.Context, req AskConsoleSessionRequest) ConsoleAskUpdate {
		response, err := submitter.SubmitAsk(ctx, req)
		if err != nil {
			return ConsoleAskUpdate{
				Type: ConsoleAskUpdateError,
				Failed: &ConsoleAskFailed{
					Content:       req.Content,
					CorrelationID: req.CorrelationID,
					Err:           err,
				},
			}
		}
		if !response.OK {
			message := strings.TrimSpace(response.Message)
			if message == "" {
				message = "ask request was not accepted"
			}
			return ConsoleAskUpdate{
				Type: ConsoleAskUpdateError,
				Failed: &ConsoleAskFailed{
					Content:       req.Content,
					CorrelationID: firstNonEmpty(response.CorrelationID, req.CorrelationID),
					Err:           errors.New(message),
				},
			}
		}
		return ConsoleAskUpdate{
			Type: ConsoleAskUpdateAccepted,
			Accepted: &ConsoleAskAccepted{
				Content:       req.Content,
				CorrelationID: firstNonEmpty(response.CorrelationID, req.CorrelationID),
				Message:       response.Message,
			},
		}
	}

	b, err := runtime.NewBounded(parent, requestBufferSize, updateBufferSize, handler)
	if err != nil {
		return nil, err
	}
	return &ConsoleAskRuntime{bounded: b}, nil
}

// Enqueue validates and adds one ask request to the bounded runtime queue.
func (rt *ConsoleAskRuntime) Enqueue(ctx context.Context, req AskConsoleSessionRequest) error {
	if rt == nil {
		return errors.New("console ask runtime is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sanitized, err := sanitizeAskConsoleSessionRequest(req)
	if err != nil {
		return err
	}

	err = rt.bounded.Enqueue(ctx, sanitized)
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
func (rt *ConsoleAskRuntime) Updates() <-chan ConsoleAskUpdate {
	return rt.bounded.Updates()
}

// Stop cancels processing and waits for the worker goroutine to exit.
func (rt *ConsoleAskRuntime) Stop() {
	if rt == nil {
		return
	}
	rt.bounded.Stop()
}

// Close is an alias for Stop.
func (rt *ConsoleAskRuntime) Close() {
	rt.Stop()
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
