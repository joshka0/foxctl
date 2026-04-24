package runner

import (
	"context"
	"fmt"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func (p *Pipeline) stageResolveDependencies(_ context.Context, st *executionState) *v2errors.V2Error {
	if p.cfg.EventStore == nil {
		return &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "event appender is required",
			Fatal:   true,
		}
	}
	switch st.in.Backend {
	case run.TurnBackendLLMChat:
		if p.cfg.Model == nil {
			return &v2errors.V2Error{
				Kind:    v2errors.ErrDependency,
				Message: "model is required",
				Fatal:   true,
			}
		}
		if p.cfg.ToolExecutor == nil {
			return &v2errors.V2Error{
				Kind:    v2errors.ErrDependency,
				Message: "tool executor is required",
				Fatal:   true,
			}
		}
	case run.TurnBackendRLMREPL:
		if p.cfg.RLMREPLFactory == nil {
			return &v2errors.V2Error{
				Kind:    v2errors.ErrDependency,
				Message: "rlm_repl runner factory is required",
				Fatal:   true,
			}
		}
	default:
		return &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: fmt.Sprintf("unsupported backend %q", st.in.Backend),
			Fatal:   true,
			Details: map[string]any{
				"field":   "backend",
				"backend": string(st.in.Backend),
			},
		}
	}
	return nil
}
