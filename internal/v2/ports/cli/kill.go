package cli

import (
	"context"

	"github.com/jkatigb/agentctl/internal/v2/core/kill"
	"github.com/jkatigb/agentctl/internal/v2/ports"
)

// KillService is the minimal kill service contract used by CLI routing.
type KillService interface {
	Kill(ctx context.Context, req kill.Request) (kill.Response, error)
}

// Kill routes one kill request by command flag.
func Kill(
	ctx context.Context,
	router Router,
	req kill.Request,
	v1 KillService,
	v2 KillService,
) (kill.Response, ports.Decision, error) {
	return Dispatch(ctx, router, "kill", req.RequestID,
		func(ctx context.Context) (kill.Response, error) {
			return v1.Kill(ctx, req)
		},
		func(ctx context.Context) (kill.Response, error) {
			return v2.Kill(ctx, req)
		},
	)
}
