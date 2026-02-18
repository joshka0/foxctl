package cli

import (
	"context"

	"github.com/jkatigb/agentctl/internal/v2/core/list"
	"github.com/jkatigb/agentctl/internal/v2/ports"
)

// ListService is the minimal list service contract used by CLI routing.
type ListService interface {
	List(ctx context.Context, req list.Request) (list.Response, error)
}

// List routes one list request by command flag.
func List(
	ctx context.Context,
	router Router,
	req list.Request,
	v1 ListService,
	v2 ListService,
) (list.Response, ports.Decision, error) {
	return Dispatch(ctx, router, "list", "",
		func(ctx context.Context) (list.Response, error) {
			return v1.List(ctx, req)
		},
		func(ctx context.Context) (list.Response, error) {
			return v2.List(ctx, req)
		},
	)
}
