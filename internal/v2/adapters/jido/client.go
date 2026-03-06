package jido

import "context"

// Client is the bridge transport contract between the v2 control plane and a
// Jido runtime process.
type Client interface {
	Health(ctx context.Context) (HealthResponse, error)
	StartAgent(ctx context.Context, req StartAgentRequest) (StartAgentResponse, error)
	StopAgent(ctx context.Context, req StopAgentRequest) (StopAgentResponse, error)
	Signal(ctx context.Context, req SignalRequest) (SignalResponse, error)
	SpawnChild(ctx context.Context, req SignalRequest) (SignalResponse, error)
	Await(ctx context.Context, req AwaitRequest) (AwaitResponse, error)
	GetChildren(ctx context.Context, req GetChildrenRequest) (GetChildrenResponse, error)
	State(ctx context.Context, req StateRequest) (StateResponse, error)
}
