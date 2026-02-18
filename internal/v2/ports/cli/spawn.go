package cli

import (
	"context"

	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
	"github.com/jkatigb/agentctl/internal/v2/ports"
)

// SpawnService is the minimal spawn service contract used by CLI routing.
type SpawnService interface {
	Spawn(ctx context.Context, req spawn.Request) (spawn.Response, error)
}

// Spawn routes one spawn request by command flag.
func Spawn(
	ctx context.Context,
	router Router,
	req spawn.Request,
	v1 SpawnService,
	v2 SpawnService,
) (spawn.Response, ports.Decision, error) {
	return Dispatch(ctx, router, "spawn", req.RequestID,
		func(ctx context.Context) (spawn.Response, error) {
			return v1.Spawn(ctx, req)
		},
		func(ctx context.Context) (spawn.Response, error) {
			return v2.Spawn(ctx, req)
		},
	)
}
