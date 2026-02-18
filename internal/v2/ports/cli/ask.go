package cli

import (
	"context"

	"github.com/jkatigb/agentctl/internal/v2/core/ask"
	"github.com/jkatigb/agentctl/internal/v2/ports"
)

// AskService is the minimal ask service contract used by CLI routing.
type AskService interface {
	Ask(ctx context.Context, req ask.Request) (ask.Response, error)
}

// Ask routes one ask request by command flag.
func Ask(
	ctx context.Context,
	router Router,
	req ask.Request,
	v1 AskService,
	v2 AskService,
) (ask.Response, ports.Decision, error) {
	return Dispatch(ctx, router, "ask", req.RequestID,
		func(ctx context.Context) (ask.Response, error) {
			return v1.Ask(ctx, req)
		},
		func(ctx context.Context) (ask.Response, error) {
			return v2.Ask(ctx, req)
		},
	)
}
