package runner

import (
	"context"
	"strings"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

func (p *Pipeline) stagePersistTurn(ctx context.Context, st *executionState) *v2errors.V2Error {
	if err := ctx.Err(); err != nil {
		return contextError(StagePersistTurn, err)
	}
	st.turn.FinalOutput = run.MessageRef{
		ID:   "msg-final",
		Role: "assistant",
		Text: strings.TrimSpace(st.out.Summary),
	}

	if p.cfg.TurnRecorder != nil {
		if err := p.cfg.TurnRecorder.SaveTurn(ctx, st.turn.Clone()); err != nil {
			return &v2errors.V2Error{
				Kind:      v2errors.ErrDependency,
				Message:   "persist turn record",
				Cause:     err,
				Fatal:     true,
				Retryable: true,
			}
		}
	}

	return p.appendEvent(ctx, st, StagePersistTurn, events.EventTurnRecorded, events.TurnRecordedPayload{
		TurnID:     st.out.TurnID,
		Iterations: st.out.Iterations,
		ToolCalls:  st.out.ToolCalls,
	})
}
