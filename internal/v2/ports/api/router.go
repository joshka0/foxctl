package api

import (
	"context"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/ports"
	portconfig "github.com/jkatigb/agentctl/internal/v2/ports/config"
)

// Router routes API commands to v1/v2 handlers based on feature flags.
type Router struct {
	flags         portconfig.V2Flags
	shadowFlags   portconfig.V2Flags
	observe       ports.Observer
	shadowObserve ports.ShadowObserver
	shadowTimeout time.Duration
}

// NewRouter builds an API router.
func NewRouter(flags portconfig.V2Flags, observe ports.Observer) Router {
	return Router{
		flags:   flags,
		observe: observe,
	}
}

// NewRouterWithShadow builds an API router with optional shadow execution.
func NewRouterWithShadow(
	flags portconfig.V2Flags,
	shadowFlags portconfig.V2Flags,
	observe ports.Observer,
	shadowObserve ports.ShadowObserver,
	shadowTimeout time.Duration,
) Router {
	return Router{
		flags:         flags,
		shadowFlags:   shadowFlags,
		observe:       observe,
		shadowObserve: shadowObserve,
		shadowTimeout: shadowTimeout,
	}
}

// Enabled reports whether a command is routed to v2.
func (r Router) Enabled(command string) bool {
	return r.flags.Enabled(strings.TrimSpace(command))
}

// Dispatch selects v1/v2 API handlers.
func Dispatch[T any](
	ctx context.Context,
	router Router,
	command string,
	correlationID string,
	v1 ports.Runner[T],
	v2 ports.Runner[T],
) (T, ports.Decision, error) {
	return ports.DispatchWithShadow(ctx, ports.DispatchOptions[T]{
		Flags:         router.flags,
		ShadowFlags:   router.shadowFlags,
		Command:       command,
		CorrelationID: correlationID,
		V1:            v1,
		V2:            v2,
		Observe:       router.observe,
		ShadowObserve: router.shadowObserve,
		ShadowTimeout: router.shadowTimeout,
	})
}
