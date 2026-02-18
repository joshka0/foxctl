package cli

import (
	"context"

	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/ports"
)

// RunService is the minimal run service contract used by CLI routing.
type RunService interface {
	Run(ctx context.Context, req run.TurnInput) (run.TurnOutput, error)
}

// Run routes one run request by command flag.
func Run(
	ctx context.Context,
	router Router,
	req run.TurnInput,
	v1 RunService,
	v2 RunService,
) (run.TurnOutput, ports.Decision, error) {
	return Dispatch(ctx, router, "run", req.RequestID,
		func(ctx context.Context) (run.TurnOutput, error) {
			return v1.Run(ctx, req)
		},
		func(ctx context.Context) (run.TurnOutput, error) {
			return v2.Run(ctx, req)
		},
	)
}
