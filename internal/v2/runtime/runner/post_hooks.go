package runner

import (
	"context"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
)

func (p *Pipeline) stageApplyPostHooks(ctx context.Context, st *executionState) *v2errors.V2Error {
	if p.cfg.Hooks == nil {
		return nil
	}
	if err := p.cfg.Hooks.RunPostHooks(ctx, st.in, st.out); err != nil {
		return asStageError(StageApplyPostHooks, err, true)
	}
	return nil
}
