package runner

import (
	"context"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
)

func (p *Pipeline) stageResolveDependencies(_ context.Context, _ *executionState) *v2errors.V2Error {
	if p.cfg.EventStore == nil {
		return &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "event appender is required",
			Fatal:   true,
		}
	}
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
	return nil
}
