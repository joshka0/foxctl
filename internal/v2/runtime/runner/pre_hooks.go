package runner

import (
	"context"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
)

func (p *Pipeline) stageApplyPreHooks(ctx context.Context, st *executionState) *v2errors.V2Error {
	if p.cfg.Hooks == nil {
		return nil
	}
	if err := p.cfg.Hooks.RunPreHooks(ctx, st.in); err != nil {
		return asStageError(StageApplyPreHooks, err, true)
	}
	return nil
}
