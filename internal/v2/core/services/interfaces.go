package services

import (
	"context"

	"github.com/jkatigb/agentctl/internal/v2/core/ask"
	"github.com/jkatigb/agentctl/internal/v2/core/kill"
	"github.com/jkatigb/agentctl/internal/v2/core/list"
	"github.com/jkatigb/agentctl/internal/v2/core/orchestration"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
)

// SpawnService handles spawn orchestration.
type SpawnService interface {
	Spawn(ctx context.Context, req spawn.Request) (spawn.Response, error)
}

// AskService handles ask message orchestration.
type AskService interface {
	Ask(ctx context.Context, req ask.Request) (ask.Response, error)
}

// RunService handles direct run orchestration.
type RunService interface {
	Run(ctx context.Context, req run.TurnInput) (run.TurnOutput, error)
}

// ListService handles projected run listing.
type ListService interface {
	List(ctx context.Context, req list.Request) (list.Response, error)
}

// KillService handles kill orchestration with id-map fallback.
type KillService interface {
	Kill(ctx context.Context, req kill.Request) (kill.Response, error)
}

// OrchestrationService handles issue dispatch and Kanban projection commands.
type OrchestrationService interface {
	DispatchIssue(ctx context.Context, req orchestration.DispatchRequest) (orchestration.DispatchResponse, error)
	Board(ctx context.Context, req orchestration.BoardRequest) (orchestration.BoardResponse, error)
	Card(ctx context.Context, req orchestration.CardRequest) (orchestration.CardResponse, error)
	Refresh(ctx context.Context, req orchestration.RefreshRequest) (orchestration.RefreshResponse, error)
}
