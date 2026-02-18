package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/ports"
	portconfig "github.com/jkatigb/agentctl/internal/v2/ports/config"
)

// Router routes daemon methods to v1/v2 handlers by per-command flags.
type Router struct {
	flags         portconfig.V2Flags
	shadowFlags   portconfig.V2Flags
	observe       ports.Observer
	shadowObserve ports.ShadowObserver
	shadowTimeout time.Duration
}

// NewRouter builds a daemon router.
func NewRouter(flags portconfig.V2Flags, observe ports.Observer) Router {
	return Router{
		flags:   flags,
		observe: observe,
	}
}

// NewRouterWithShadow builds a daemon router with optional shadow execution.
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

// CommandForMethod maps daemon method names to v2 command keys.
func CommandForMethod(method string) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(method)) {
	case "agent.spawn":
		return "spawn", true
	case "agent.ask":
		return "ask", true
	case "agent.run":
		return "run", true
	case "agent.list":
		return "list", true
	case "agent.kill":
		return "kill", true
	default:
		return "", false
	}
}

// DispatchMethod routes daemon method handlers through the same flag logic.
func DispatchMethod[T any](
	ctx context.Context,
	router Router,
	method string,
	correlationID string,
	v1 ports.Runner[T],
	v2 ports.Runner[T],
) (T, ports.Decision, error) {
	command, ok := CommandForMethod(method)
	if !ok {
		// Unknown methods intentionally remain on v1 path while preserving
		// method-level observability.
		if router.observe != nil {
			router.observe(strings.TrimSpace(strings.ToLower(method)), ports.DecisionV1, strings.TrimSpace(correlationID))
		}
		if v1 == nil {
			var zero T
			return zero, ports.DecisionV1, &v2errors.V2Error{
				Kind:    v2errors.ErrDependency,
				Message: fmt.Sprintf("v1 daemon handler is not configured for method %q", method),
				Fatal:   true,
			}
		}
		out, err := v1(ctx)
		return out, ports.DecisionV1, err
	}
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
