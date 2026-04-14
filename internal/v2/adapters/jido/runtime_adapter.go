package jido

import (
	"context"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/v2/core/ask"
)

// RuntimeAdapterConfig configures a RuntimeAdapter.
type RuntimeAdapterConfig struct {
	Client        Client
	SignalSource  string
	OnSignalAck   func(ctx context.Context, req SignalRequest, resp SignalResponse) error
	PrepareSignal func(ctx context.Context, msg ask.Message, req *SignalRequest) error
}

// RuntimeAdapter maps v2 service contracts onto bridge client calls.
type RuntimeAdapter struct {
	client        Client
	signalSource  string
	onSignalAck   func(ctx context.Context, req SignalRequest, resp SignalResponse) error
	prepareSignal func(ctx context.Context, msg ask.Message, req *SignalRequest) error
}

// NewRuntimeAdapter builds a bridge adapter for v2 services.
func NewRuntimeAdapter(cfg RuntimeAdapterConfig) (*RuntimeAdapter, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("jido client is required")
	}
	src := strings.TrimSpace(cfg.SignalSource)
	if src == "" {
		src = DefaultSignalSource
	}
	return &RuntimeAdapter{
		client:        cfg.Client,
		signalSource:  src,
		onSignalAck:   cfg.OnSignalAck,
		prepareSignal: cfg.PrepareSignal,
	}, nil
}

// Send dispatches one normalized ask message to the runtime.
func (a *RuntimeAdapter) Send(ctx context.Context, msg ask.Message) (string, error) {
	if a == nil || a.client == nil {
		return "", fmt.Errorf("jido runtime adapter is not configured")
	}

	req, err := AskMessageToSignalRequest(msg, a.signalSource)
	if err != nil {
		return "", err
	}
	if a.prepareSignal != nil {
		if err := a.prepareSignal(ctx, msg, &req); err != nil {
			return "", err
		}
	}
	req.Mode = NormalizeSignalMode(req.Mode)

	resp, err := a.client.Signal(ctx, req)
	if err != nil {
		return "", err
	}
	if a.onSignalAck != nil {
		if ackErr := a.onSignalAck(ctx, req, resp); ackErr != nil {
			return "", ackErr
		}
	}

	return MessageIDFromSignalResponse(resp, msg.AskID), nil
}

// Health returns runtime health details.
func (a *RuntimeAdapter) Health(ctx context.Context) (HealthResponse, error) {
	if a == nil || a.client == nil {
		return HealthResponse{}, fmt.Errorf("jido runtime adapter is not configured")
	}
	return a.client.Health(ctx)
}

// StartAgent forwards runtime start requests.
func (a *RuntimeAdapter) StartAgent(ctx context.Context, req StartAgentRequest) (StartAgentResponse, error) {
	if a == nil || a.client == nil {
		return StartAgentResponse{}, fmt.Errorf("jido runtime adapter is not configured")
	}
	return a.client.StartAgent(ctx, req)
}

// StopAgent forwards runtime stop requests.
func (a *RuntimeAdapter) StopAgent(ctx context.Context, req StopAgentRequest) (StopAgentResponse, error) {
	if a == nil || a.client == nil {
		return StopAgentResponse{}, fmt.Errorf("jido runtime adapter is not configured")
	}
	return a.client.StopAgent(ctx, req)
}

// SpawnChild forwards runtime child-spawn requests.
func (a *RuntimeAdapter) SpawnChild(ctx context.Context, req SignalRequest) (SignalResponse, error) {
	if a == nil || a.client == nil {
		return SignalResponse{}, fmt.Errorf("jido runtime adapter is not configured")
	}
	return a.client.SpawnChild(ctx, req)
}

// Await forwards completion waits.
func (a *RuntimeAdapter) Await(ctx context.Context, req AwaitRequest) (AwaitResponse, error) {
	if a == nil || a.client == nil {
		return AwaitResponse{}, fmt.Errorf("jido runtime adapter is not configured")
	}
	return a.client.Await(ctx, req)
}

// GetChildren forwards child lookup requests.
func (a *RuntimeAdapter) GetChildren(ctx context.Context, req GetChildrenRequest) (GetChildrenResponse, error) {
	if a == nil || a.client == nil {
		return GetChildrenResponse{}, fmt.Errorf("jido runtime adapter is not configured")
	}
	return a.client.GetChildren(ctx, req)
}

// State forwards runtime state requests.
func (a *RuntimeAdapter) State(ctx context.Context, req StateRequest) (StateResponse, error) {
	if a == nil || a.client == nil {
		return StateResponse{}, fmt.Errorf("jido runtime adapter is not configured")
	}
	return a.client.State(ctx, req)
}
